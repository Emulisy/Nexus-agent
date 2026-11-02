-- Seed provider endpoints and built-in tool descriptions after the schema exists.
-- Unique codes and ON CONFLICT DO NOTHING preserve existing rows during migration.
-- cmd/migrate bootstraps the administrator; service startup separately creates
-- missing chat presets and synchronizes nonempty environment credentials.

-- Credentials are not embedded here. The config loader fills environment values
-- from .env files, and model startup encrypts configured keys before persistence.
INSERT INTO model_providers (code, name, base_url) VALUES
  ('openai',   'OpenAI',   'https://api.openai.com/v1'),
  ('gemini',   'Gemini',   'https://generativelanguage.googleapis.com/v1beta/openai'),
  ('deepseek', 'DeepSeek', 'https://api.deepseek.com')
ON CONFLICT (code) DO NOTHING;

-- Descriptions and parameter schemas are included in model requests when an Agent
-- exposes these tools. The Go registry supplies implementations; timeout_ms bounds
-- invocation, and is_dangerous is classification metadata rather than an approval rule.
INSERT INTO tools (code, name, type, description, parameters, handler, timeout_ms, is_dangerous) VALUES
('calculator', 'Calculator', 'builtin',
 'Evaluate arithmetic expressions, such as (1+2)*3',
 '{"type":"object","properties":{"expression":{"type":"string","description":"Mathematical expression"}},"required":["expression"]}',
 'calculator', 5000, FALSE),
('datetime', 'Date and time', 'builtin',
 'Get the current date and time, convert time zones, and calculate dates',
 '{"type":"object","properties":{"operation":{"type":"string","enum":["now","add_days","diff"],"description":"Operation type"},"tz":{"type":"string","description":"Time zone, defaults to Asia/Shanghai"},"days":{"type":"integer"}},"required":["operation"]}',
 'datetime', 3000, FALSE),
('web_search', 'Web search', 'builtin',
 'Search the web for current information and return titles, links, and summaries. Use for time-sensitive questions',
 '{"type":"object","properties":{"query":{"type":"string","description":"Search query"},"count":{"type":"integer","description":"Number of results, defaults to 5"}},"required":["query"]}',
 'web_search', 15000, FALSE),
('http_client', 'HTTP client', 'builtin',
 'Send a GET request to an allowed domain and return the response content',
 '{"type":"object","properties":{"url":{"type":"string","description":"Complete URL"},"headers":{"type":"object"}},"required":["url"]}',
 'http_client', 15000, FALSE),
('code_exec', 'Code execution', 'builtin',
 'Run Python, Go, or shell code in an isolated sandbox and return stdout, stderr, and the exit code',
 '{"type":"object","properties":{"language":{"type":"string","enum":["python","go","shell"]},"code":{"type":"string","description":"Complete source code"}},"required":["language","code"]}',
 'code_exec', 35000, TRUE),
('file_ops', 'File operations', 'builtin',
 'Read or save conversation attachments within authorized directories',
 '{"type":"object","properties":{"operation":{"type":"string","enum":["read","save"]},"path":{"type":"string"},"content":{"type":"string"}},"required":["operation","path"]}',
 'file_ops', 10000, TRUE),
('db_query', 'Database query', 'builtin',
 'Run read-only SELECT queries against the business database for data analysis',
 '{"type":"object","properties":{"sql":{"type":"string","description":"Read-only SELECT statement"}},"required":["sql"]}',
 'db_query', 10000, TRUE),
('kb_search', 'Knowledge base search', 'builtin',
 'Search a knowledge base using hybrid retrieval and return the most relevant document chunks',
 '{"type":"object","properties":{"kb_id":{"type":"integer","description":"Knowledge base ID"},"query":{"type":"string"},"top_k":{"type":"integer"}},"required":["kb_id","query"]}',
 'kb_search', 10000, FALSE)
ON CONFLICT (code) DO NOTHING;
