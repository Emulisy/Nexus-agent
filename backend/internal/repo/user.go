// User persistence supports account lookup and serialized account creation.
// First-admin selection and duplicate checks occur inside the same transaction;
// the service remains responsible for input validation and password hashing.
package repo

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/Emulisy/SupaAgent/internal/model"
)

// ErrUserAlreadyExists identifies a duplicate username/email detected inside the registration transaction.
var ErrUserAlreadyExists = errors.New("user already exists")

// UserRepo reads user records and inserts accounts under the registration lock.
type UserRepo struct {
	db *gorm.DB
}

// CreateFirstOrUser serializes registrations using an exclusive users-table lock.
// Within that transaction it counts existing users, repeats the duplicate check,
// assigns admin only when the table is empty, and inserts the supplied bcrypt hash.
// The lock is released on commit/rollback, including any error path.
func (r *UserRepo) CreateFirstOrUser(ctx context.Context, u *model.User) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("LOCK TABLE users IN EXCLUSIVE MODE").Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&model.User{}).Count(&count).Error; err != nil {
			return err
		}
		var exists int64
		if err := tx.Model(&model.User{}).
			Where("username = ? OR email = ?", u.Username, u.Email).
			Count(&exists).Error; err != nil {
			return err
		}
		if exists > 0 {
			return ErrUserAlreadyExists
		}
		if count == 0 {
			u.Role = "admin"
		} else {
			u.Role = "user"
		}
		return tx.Create(u).Error
	})
}

// GetByID loads the account used by profile endpoints and returns GORM lookup errors unchanged.
func (r *UserRepo) GetByID(ctx context.Context, id int64) (*model.User, error) {
	var u model.User
	if err := r.db.WithContext(ctx).First(&u, id).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// GetByUsername loads the credential-bearing record needed for login; it does not validate the password.
func (r *UserRepo) GetByUsername(ctx context.Context, username string) (*model.User, error) {
	var u model.User
	if err := r.db.WithContext(ctx).Where("username = ?", username).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// ExistsBy checks username/email collisions before hashing; creation repeats this check under lock.
func (r *UserRepo) ExistsBy(ctx context.Context, username, email string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.User{}).
		Where("username = ? OR email = ?", username, email).
		Count(&count).Error
	return count > 0, err
}
