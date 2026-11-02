// Authentication uses bcrypt password hashes and short-lived access JWTs.
// Refresh tokens are allowlisted in Redis per user and rotated atomically on use.
// Account creation delegates first-admin selection and duplicate checks to a
// database transaction so concurrent registrations cannot both become admins.
package service

import (
	"context"
	"errors"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/Emulisy/SupaAgent/internal/model"
	"github.com/Emulisy/SupaAgent/internal/pkg/errcode"
	"github.com/Emulisy/SupaAgent/internal/repo"
	"github.com/Emulisy/SupaAgent/internal/security"
)

// rotateRefreshTokenScript consumes an old refresh token before adding its replacement.
// The Redis script makes that transition atomic, so a concurrently replayed
// token cannot succeed twice. Expiration applies to the user's entire set key.
var rotateRefreshTokenScript = redis.NewScript(`
if redis.call('SREM', KEYS[1], ARGV[1]) == 0 then
	return 0
end
redis.call('SADD', KEYS[1], ARGV[2])
redis.call('PEXPIRE', KEYS[1], ARGV[3])
return 1
`)

// AuthService coordinates user persistence, JWT issuance, and Redis refresh-token state.
type AuthService struct {
	repos *repo.Repos
	users *repo.UserRepo
	jwt   *security.JWTManager
	rdb   *redis.Client
}

// NewAuthService binds account storage, token signing, and the shared refresh-token store.
func NewAuthService(repos *repo.Repos, jwt *security.JWTManager, rdb *redis.Client) *AuthService {
	return &AuthService{repos: repos, users: repos.User, jwt: jwt, rdb: rdb}
}

// RegisterInput carries plaintext credentials only for validation and hashing.
// Register applies its own length and email checks before accessing storage.
type RegisterInput struct {
	Username string `json:"username" vd:"len($)>2"`
	Email    string `json:"email"`
	Password string `json:"password" vd:"len($)>7"`
}

// Register normalizes account identifiers and validates password/email bounds.
// A preliminary duplicate lookup avoids unnecessary hashing, while the repository
// transaction repeats the uniqueness check and assigns the first account's role.
// Only the bcrypt hash is persisted; the returned user excludes it from JSON.
func (s *AuthService) Register(ctx context.Context, in *RegisterInput) (*model.User, error) {
	in.Username = strings.TrimSpace(in.Username)
	in.Email = strings.TrimSpace(in.Email)
	if len(in.Username) < 3 || len(in.Username) > 64 {
		return nil, errcode.ErrInvalidParam.WithMsg("Username must be between 3 and 64 bytes long")
	}
	if addr, err := mail.ParseAddress(in.Email); err != nil || addr.Address != in.Email || len(in.Email) > 128 {
		return nil, errcode.ErrInvalidParam.WithMsg("Please provide a valid email address")
	}
	if len(in.Password) < 12 || len(in.Password) > 72 {
		return nil, errcode.ErrInvalidParam.WithMsg("Password must be between 12 and 72 bytes long")
	}
	if exists, err := s.users.ExistsBy(ctx, in.Username, in.Email); err != nil {
		return nil, errcode.ErrInternal.WithCause(err)
	} else if exists {
		return nil, errcode.ErrUserExists
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, errcode.ErrInternal.WithCause(err)
	}

	u := &model.User{Username: in.Username, Email: in.Email, PasswordHash: string(hash)}
	if err := s.users.CreateFirstOrUser(ctx, u); err != nil {
		if errors.Is(err, repo.ErrUserAlreadyExists) {
			return nil, errcode.ErrUserExists
		}
		return nil, errcode.ErrInternal.WithCause(err)
	}
	return u, nil
}

// Login checks account status and bcrypt credentials before generating tokens.
// Unknown usernames and wrong passwords share the same credential error. The
// refresh token must be stored in Redis before the token pair is returned.
func (s *AuthService) Login(ctx context.Context, username, password string) (*security.TokenPair, error) {
	u, err := s.users.GetByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.ErrLoginFailed
		}
		return nil, errcode.ErrInternal.WithCause(err)
	}
	if u.Status != 1 {
		return nil, errcode.ErrUserLocked
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, errcode.ErrLoginFailed
	}
	pair, err := s.jwt.Generate(u.ID, u.Role, u.Username)
	if err != nil {
		return nil, errcode.ErrInternal.WithCause(err)
	}
	if err := s.StoreRefreshToken(ctx, pair.UserID, pair.RefreshToken, s.jwt.RefreshTTL()); err != nil {
		return nil, errcode.ErrInternal.WithCause(err)
	}
	return pair, nil
}

// Refresh validates the refresh issuer/signature/expiry, then atomically rotates its allowlist entry.
// An absent or previously consumed entry becomes an invalid-token error. New
// claims are copied from the validated token rather than reloading the user row.
func (s *AuthService) Refresh(ctx context.Context, refreshToken string) (*security.TokenPair, error) {
	claims, err := s.jwt.Parse(refreshToken, true)
	if err != nil {
		return nil, errcode.ErrTokenInvalid
	}
	pair, err := s.jwt.Generate(claims.UserID, claims.Role, claims.Username)
	if err != nil {
		return nil, errcode.ErrInternal.WithCause(err)
	}
	rotated, err := rotateRefreshTokenScript.Run(ctx, s.rdb,
		[]string{refreshKey(claims.UserID)}, refreshToken, pair.RefreshToken,
		s.jwt.RefreshTTL().Milliseconds()).Int()
	if err != nil {
		return nil, errcode.ErrInternal.WithCause(err)
	}
	if rotated != 1 {
		return nil, errcode.ErrTokenInvalid.WithMsg("Refresh token is no longer valid")
	}
	return pair, nil
}

// Logout deletes all allowlisted refresh tokens for the user.
// Previously issued access JWTs remain valid until their own expiry because the
// access-token middleware checks signatures and claims rather than this set.
func (s *AuthService) Logout(ctx context.Context, userID int64) error {
	return s.rdb.Del(ctx, refreshKey(userID)).Err()
}

// StoreRefreshToken adds a token to the user's set and applies the supplied key TTL.
// The pipeline groups the Redis commands but is not a cross-system transaction
// with JWT issuance or PostgreSQL changes.
func (s *AuthService) StoreRefreshToken(ctx context.Context, userID int64, token string, ttl time.Duration) error {
	key := refreshKey(userID)
	pipe := s.rdb.Pipeline()
	pipe.SAdd(ctx, key, token)
	pipe.Expire(ctx, key, ttl)
	_, err := pipe.Exec(ctx)
	return err
}

// refreshKey namespaces each user's refresh-token allowlist independently in Redis.
func refreshKey(userID int64) string {
	return "nx:auth:refresh:" + strconv.FormatInt(userID, 10)
}
