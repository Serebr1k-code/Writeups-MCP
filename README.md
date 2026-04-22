# Writeups MCP for OpenCode

MCP сервер для поиска и чтения CTF writeups и другой документации по безопасности.

## Содержимое базы

- CTF writeups (Writeups Team Knapsack)
- HackTricks (техники взлома)
- Описания уязвимостей
- Руководства по эксплуатации
- Читшиты и шпаргалки
- Документация по безопасности

## Быстрый старт

### 1. Запуск MCP

```bash
cd /home/Serebr1k/writeups-mcp-opencode
node index.js
```

### 2. Использование инструментов

#### search_writeups - Поиск

```javascript
// Поиск по ключевым словам
{tool: "search_writeups", args: {query: "SQL injection", limit: 5}}

// Поиск техники
{tool: "search_writeups", args: {query: "privilege escalation windows"}}

// Поиск CVE
{tool: "search_writeups", args: {query: "CVE-2024-1709"}}
```

Возвращает нумерованный список `[1]`, `[2]`, `[3]`...

#### read_writeup - Чтение

```javascript
// Чтение по ID из поиска
{tool: "read_writeup", args: {id: 1}}

// Чтение с диапазоном строк
{tool: "read_writeup", args: {id: 1, lines: "1-50"}}

// Чтение по пути
{tool: "read_writeup", args: {path: "/home/Serebr1k/kraber/knowledge_base/..."}}

// Чтение вокруг конкретной строки
{tool: "read_writeup", args: {id: 1, lines: "100"}}
```

#### help - Справка

```javascript
// Полная справка
{tool: "help", args: {tool: "all"}}

// Только про поиск
{tool: "help", args: {tool: "search"}}

// Только про чтение
{tool: "help", args: {tool: "read"}}
```

## Интеграция с OpenCode

Добавь в `.config/opencode/config.json`:

```json
"writeups-mcp": {
  "enabled": true,
  "type": "local",
  "command": ["node", "/home/Serebr1k/writeups-mcp-opencode/index.js"],
  "environment": {
    "WRITEUPS_DB": "/home/Serebr1k/writeups-mcp-opencode/data/writeups_index.db"
  },
  "workingDirectory": "/home/Serebr1k/writeups-mcp-opencode"
}
```

## Пример использования агентом

```
Агент: Найди информацию про SQL injection
-> search_writeups({query: "SQL injection", limit: 5})

Результат:
[1] SQLite Injection (lines ~1-11)
Path: /home/Serebr1k/kraber/knowledge_base/SQL Injection/SQLite Injection.md
---snippet---
...

[2] MSSQL Injection (lines ~1-11)
...

Агент: Прочитай первый результат
-> read_writeup({id: 1, lines: "1-30"})

Содержимое файла...
```

## Переменные окружения

- `WRITEUPS_DB` - путь к SQLite базе с индексом (по умолчанию: ~/writeups-mcp-opencode/data/writeups_index.db)