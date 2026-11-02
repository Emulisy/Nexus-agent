-- Restore the prior defaults only where the English preset text is unchanged.
ALTER TABLE sessions ALTER COLUMN title SET DEFAULT '新对话';
UPDATE sessions SET title = '新对话' WHERE title = 'New chat' AND message_count = 0;

UPDATE model_providers AS provider
SET name = preset.old_name
FROM (VALUES
    ('qwen', '通义千问', 'Qwen'),
    ('zhipu', '智谱AI', 'Zhipu AI'),
    ('doubao', '豆包Ark', 'Doubao Ark'),
    ('ollama', 'Ollama本地', 'Ollama Local')
) AS preset(code, old_name, new_name)
WHERE provider.code = preset.code AND provider.name = preset.new_name;

-- Replace only unchanged built-in text; preserve customized names, descriptions, and schemas.
UPDATE tools AS tool
SET name = CASE WHEN tool.name = preset.old_name THEN preset.new_name ELSE tool.name END,
    description = CASE WHEN tool.description = preset.old_description THEN preset.new_description ELSE tool.description END,
    parameters = CASE WHEN tool.parameters = preset.old_parameters::jsonb THEN preset.new_parameters::jsonb ELSE tool.parameters END
FROM (VALUES
    ('calculator', 'Calculator', 'Evaluate arithmetic expressions, such as (1+2)*3', '{"type":"object","properties":{"expression":{"type":"string","description":"Mathematical expression"}},"required":["expression"]}', '计算器', '执行四则运算表达式求值，输入合法的数学表达式，例如 (1+2)*3', '{"type":"object","properties":{"expression":{"type":"string","description":"数学表达式"}},"required":["expression"]}'),
    ('datetime', 'Date and time', 'Get the current date and time, convert time zones, and calculate dates', '{"type":"object","properties":{"operation":{"type":"string","enum":["now","add_days","diff"],"description":"Operation type"},"tz":{"type":"string","description":"Time zone, defaults to Asia/Shanghai"},"days":{"type":"integer"}},"required":["operation"]}', '时间日期', '获取当前日期时间、时区转换、日期推算', '{"type":"object","properties":{"operation":{"type":"string","enum":["now","add_days","diff"],"description":"操作类型"},"tz":{"type":"string","description":"时区，默认 Asia/Shanghai"},"days":{"type":"integer"}},"required":["operation"]}'),
    ('web_search', 'Web search', 'Search the web for current information and return titles, links, and summaries. Use for time-sensitive questions', '{"type":"object","properties":{"query":{"type":"string","description":"Search query"},"count":{"type":"integer","description":"Number of results, defaults to 5"}},"required":["query"]}', '网络搜索', '联网搜索最新信息，返回标题、链接与摘要。回答时效性问题时使用', '{"type":"object","properties":{"query":{"type":"string","description":"搜索关键词"},"count":{"type":"integer","description":"结果条数，默认5"}},"required":["query"]}'),
    ('http_client', 'HTTP client', 'Send a GET request to an allowed domain and return the response content', '{"type":"object","properties":{"url":{"type":"string","description":"Complete URL"},"headers":{"type":"object"}},"required":["url"]}', 'HTTP请求', '向白名单域名发起 GET 请求并返回响应内容', '{"type":"object","properties":{"url":{"type":"string","description":"完整 URL"},"headers":{"type":"object"}},"required":["url"]}'),
    ('code_exec', 'Code execution', 'Run Python, Go, or shell code in an isolated sandbox and return stdout, stderr, and the exit code', '{"type":"object","properties":{"language":{"type":"string","enum":["python","go","shell"]},"code":{"type":"string","description":"Complete source code"}},"required":["language","code"]}', '代码执行', '在隔离沙箱中执行代码，支持 python/go/shell，返回 stdout/stderr 与退出码', '{"type":"object","properties":{"language":{"type":"string","enum":["python","go","shell"]},"code":{"type":"string","description":"完整代码"}},"required":["language","code"]}'),
    ('file_ops', 'File operations', 'Read or save conversation attachments within authorized directories', '{"type":"object","properties":{"operation":{"type":"string","enum":["read","save"]},"path":{"type":"string"},"content":{"type":"string"}},"required":["operation","path"]}', '文件操作', '读取或保存会话附件文件，路径必须位于授权目录内', '{"type":"object","properties":{"operation":{"type":"string","enum":["read","save"]},"path":{"type":"string"},"content":{"type":"string"}},"required":["operation","path"]}'),
    ('db_query', 'Database query', 'Run read-only SELECT queries against the business database for data analysis', '{"type":"object","properties":{"sql":{"type":"string","description":"Read-only SELECT statement"}},"required":["sql"]}', '数据库查询', '对业务数据库执行只读 SELECT 查询，用于数据分析场景', '{"type":"object","properties":{"sql":{"type":"string","description":"只读 SELECT 语句"}},"required":["sql"]}'),
    ('kb_search', 'Knowledge base search', 'Search a knowledge base using hybrid retrieval and return the most relevant document chunks', '{"type":"object","properties":{"kb_id":{"type":"integer","description":"Knowledge base ID"},"query":{"type":"string"},"top_k":{"type":"integer"}},"required":["kb_id","query"]}', '知识库检索', '在指定知识库中进行混合检索，返回最相关的文档片段', '{"type":"object","properties":{"kb_id":{"type":"integer","description":"知识库ID"},"query":{"type":"string"},"top_k":{"type":"integer"}},"required":["kb_id","query"]}')
) AS preset(code, old_name, old_description, old_parameters, new_name, new_description, new_parameters)
WHERE tool.code = preset.code
  AND tool.type = 'builtin'
  AND (tool.name = preset.old_name OR tool.description = preset.old_description OR tool.parameters = preset.old_parameters::jsonb);
