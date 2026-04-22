#!/usr/bin/env node
/**
 * Writeups MCP Server
 * 
 * MCP server for CTF writeups knowledge base search
 * Provides tools to search through indexed writeups for techniques, vulnerabilities, and solutions
 * 
 * Database contains: CTF writeups, HackTricks, security techniques, vulnerability explanations,
 * exploitation guides, cheat sheets, and various security-related documentation
 */
import { Server } from '@modelcontextprotocol/sdk/server/index.js';
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js';
import { CallToolRequestSchema, ListToolsRequestSchema } from '@modelcontextprotocol/sdk/types.js';
import Database from 'better-sqlite3';
import path from 'path';
import os from 'os';
import fs from 'fs';

const DB_PATH = process.env.WRITEUPS_DB || path.join(os.homedir(), 'writeups-mcp-opencode', 'data', 'writeups_index.db');

class WriteupsMCPServer {
    constructor() {
        this.cache = new Map();
        this.cacheId = 1;
        
        this.server = new Server({
            name: 'Writeups MCP Server',
            version: '1.0.0'
        }, {
            capabilities: {
                tools: {
                    search_writeups: {
                        description: 'Search CTF writeups knowledge base. Database contains: CTF writeups, HackTricks, security techniques, vulnerability explanations, exploitation guides, cheat sheets. Returns numbered results [1], [2], etc. Use read_writeup with ID from results to read full content.',
                        inputSchema: {
                            type: 'object',
                            properties: {
                                query: {
                                    type: 'string',
                                    description: 'Search query: keywords (SQL injection, XSS), technique name (privilege escalation, LFI to RCE), CVE number (CVE-2024-1234), or any words from documentation. REQUIRED'
                                },
                                limit: {
                                    type: 'number',
                                    description: 'Maximum number of results to return, default 10',
                                    default: 10
                                }
                            },
                            required: ['query']
                        }
                    },
                    read_writeup: {
                        description: 'Read writeup content by ID (from search results) or by full path. ID format: number like 1, 2, 3 from [1], [2] in search results. Shows full file or specific line range. Can read around specific line or range.',
                        inputSchema: {
                            type: 'object',
                            properties: {
                                id: {
                                    type: 'number',
                                    description: 'Result ID from search (from [1], [2], etc in results). PRIORITY over path.'
                                },
                                path: {
                                    type: 'string',
                                    description: 'Full path to writeup file. Use ONLY if no id. Example: "/home/Serebr1k/kraber/knowledge_base/SQL Injection/SQLite Injection.md"'
                                },
                                lines: {
                                    type: 'string',
                                    description: 'Line range: "100-150" (show lines 100-150), "50" (show around line 50), "100-" (from line 100 to end). Default: first 100 lines',
                                    default: '1-100'
                                }
                            }
                        }
                    },
                    help: {
                        description: 'Show detailed help for writeups MCP tools usage with examples',
                        inputSchema: {
                            type: 'object',
                            properties: {
                                tool: {
                                    type: 'string',
                                    description: 'Tool name: "search" for search_help, "read" for read_help, or "all" for everything. Default: all',
                                    default: 'all'
                                }
                            }
                        }
                    }
                }
            }
        });
        
        this.server.setRequestHandler(ListToolsRequestSchema, async () => {
            return {
                tools: [
                    {
                        name: 'search_writeups',
                        description: 'Search CTF writeups knowledge base. Database contains: CTF writeups, HackTricks, security techniques, vulnerability explanations, exploitation guides, cheat sheets. Returns numbered results [1], [2], etc. Use read_writeup with ID from results to read full content.',
                        inputSchema: {
                            type: 'object',
                            properties: {
                                query: {
                                    type: 'string',
                                    description: 'Search query: keywords (SQL injection, XSS), technique name (privilege escalation, LFI to RCE), CVE number (CVE-2024-1234), or any words from documentation. REQUIRED'
                                },
                                limit: {
                                    type: 'number',
                                    description: 'Maximum number of results to return, default 10',
                                    default: 10
                                }
                            },
                            required: ['query']
                        }
                    },
                    {
                        name: 'read_writeup',
                        description: 'Read writeup content by ID (from search results) or by full path. ID format: number like 1, 2, 3 from [1], [2] in search results. Shows full file or specific line range. Can read around specific line or range.',
                        inputSchema: {
                            type: 'object',
                            properties: {
                                id: {
                                    type: 'number',
                                    description: 'Result ID from search (from [1], [2], etc in results). PRIORITY over path.'
                                },
                                path: {
                                    type: 'string',
                                    description: 'Full path to writeup file. Use ONLY if no id. Example: "/home/Serebr1k/kraber/knowledge_base/SQL Injection/SQLite Injection.md"'
                                },
                                lines: {
                                    type: 'string',
                                    description: 'Line range: "100-150" (show lines 100-150), "50" (show around line 50), "100-" (from line 100 to end). Default: first 100 lines',
                                    default: '1-100'
                                }
                            }
                        }
                    },
                    {
                        name: 'help',
                        description: 'Show detailed help for writeups MCP tools usage with examples',
                        inputSchema: {
                            type: 'object',
                            properties: {
                                tool: {
                                    type: 'string',
                                    description: 'Tool name: "search" for search_help, "read" for read_help, or "all" for everything. Default: all',
                                    default: 'all'
                                }
                            }
                        }
                    }
                ]
            };
        });
        
        this.server.setRequestHandler(CallToolRequestSchema, async (request) => {
            const name = request.params?.name;
            const args = request.params?.arguments || {};
            
            if (name === 'search_writeups') {
                if (!args.query) return { content: [{ type: 'text', text: 'query required' }] };
                return await this.search(args.query, args.limit || 10);
            } else if (name === 'read_writeup') {
                if (!args.id && !args.path) return { content: [{ type: 'text', text: 'need id or path' }] };
                return await this.readWriteup(args.id, args.path, args.lines);
            } else if (name === 'help') {
                return this.getHelp(args.tool || 'all');
            }
            return { content: [{ type: 'text', text: 'Unknown: ' + name }] };
        });
    }
    
    getHelp(tool) {
        const helpText = {
            search: `search_writeups - Поиск по базе знаний CTF writeups
==========================================================
Содержит: CTF writeups, HackTricks, техники взлома, описания уязвимостей,
руководства по эксплуатации, читшиты и др. документация по безопасности

ПАРАМЕТРЫ:
- query (string, ОБЯЗАТЕЛЬНО): Поисковый запрос
  • Ключевые слова: "SQL injection", "XSS", "buffer overflow"
  • Названия техник: "privilege escalation", "LFI to RCE"
  • CVE: "CVE-2024-1234"
  • Любые слова из документации
- limit (number, опционально): Максимум результатов (по умолчанию: 10)

ПРИМЕРЫ:
 search_writeups({query: "SQL injection", limit: 5})
 search_writeups({query: "privilege escalation windows"})
 search_writeups({query: "CVE-2024-1709"})

РЕЗУЛЬТАТ:
Возвращает нумерованный список [1], [2], [3]...
Для чтения используй read_writeup с полученным ID`,

            read: `read_writeup - Чтение содержимого райтапа
==========================================
Читает файл полностью или частично по ID из поиска или по пути

ПАРАМЕТРЫ:
- id (number, опционально): ID из результатов поиска (нап��имер [1])
  ПРИОРИТЕТНЕЕ чем path - используй этот если есть
- path (string, опционально): Полный путь к файлу
  Пример: "/home/Serebr1k/kraber/knowledge_base/SQL Injection/SQLite Injection.md"
- lines (string, опционально): Диапазон строк
  • "100-150" - показать строки 100-150
  • "50" - показать вокруг строки 50 (50-60)
  • "100-" - с строки 100 до конца
  • Без параметра - весь файл или первые 100 строк

ПРИМЕРЫ:
 read_writeup({id: 1})                          // читать результат #1
 read_writeup({id: 1, lines: "1-50"})         // строки 1-50
 read_writeup({path: "/path/to/file.md", lines: "100"}) // вокруг строки 100
 read_writeup({path: "/path/to/file.md"})          // весь файл

ОШИБКИ:
- "Must provide ID or path" - нужно указать id или path
- "File not found" - файл не существует
- "Invalid line range" - неверный формат строк`
        };
        
        if (tool === 'all') {
            return { content: [{ type: 'text', text: helpText.search + '\n\n' + helpText.read }] };
        }
        if (tool === 'search') return { content: [{ type: 'text', text: helpText.search }] };
        if (tool === 'read') return { content: [{ type: 'text', text: helpText.read }] };
        return { content: [{ type: 'text', text: 'Unknown tool: ' + tool + '. Use: search, read, or all' }] };
    }
    
    async search(searchQuery, limit) {
        try {
            const db = new Database(DB_PATH, { readonly: true });
            const stmt = db.prepare('SELECT d.rowid as id, d.title, d.path FROM docs d JOIN docs_fts f ON d.rowid = f.rowid WHERE docs_fts MATCH ? LIMIT ?');
            const rows = stmt.all(searchQuery, limit);
            
            if (rows.length === 0) return { content: [{ type: 'text', text: 'Not found: ' + searchQuery }] };
            
            let out = '=== Found ' + rows.length + ' ===\n\n';
            for (const row of rows) {
                const sr = db.prepare("SELECT snippet(docs_fts, -1, ' ', ' ', ' ', 10) as s FROM docs_fts WHERE rowid = ?").get(row.id);
                const cid = this.cacheId++;
                this.cache.set(cid, { path: row.path });
                out += cid + '. ' + row.title + '\n';
                out += '   ' + row.path.replace('/home/Serebr1k/kraber/knowledge_base/', '') + '\n';
                const snippet = (sr?.s || '').replace(/==/g, '').replace(/\n/g, ' ').substring(0, 80);
                if (snippet) out += '   ' + snippet + '...\n';
                out += '\n';
            }
            db.close();
            return { content: [{ type: 'text', text: out }] };
        } catch (e) { return { content: [{ type: 'text', text: 'Error: ' + e.message }] }; }
    }
    
async readWriteup(id, pathArg, linesArg) {
        try {
            const fp = id && this.cache.has(id) ? this.cache.get(id).path : pathArg;
            if (!fp) return { content: [{ type: 'text', text: 'Need id or path' }] };
            if (!fs.existsSync(fp)) return { content: [{ type: 'text', text: 'Not found' }] };
            
            const lines = fs.readFileSync(fp, 'utf-8').split('\n');
            let s = 1, e = Math.min(100, lines.length);
            if (linesArg) {
                if (linesArg.includes('-') && !linesArg.endsWith('-')) { const [a, b] = linesArg.split('-').map(Number); s = Math.max(1, a||1); e = Math.min(lines.length, b||lines.length); }
                else if (linesArg.endsWith('-')) { s = parseInt(linesArg) || 1; e = lines.length; }
                else { const n = parseInt(linesArg); s = Math.max(1, n - 10); e = Math.min(lines.length, n + 10); }
            }
            
            let out = path.basename(fp) + ' [lines ' + s + '-' + e + ' of ' + lines.length + ']\n';
            out += '─'.repeat(50) + '\n';
            out += lines.slice(s - 1, e).map((l, i) => (s + i).toString().padStart(4) + '| ' + l).join('\n');
            return { content: [{ type: 'text', text: out }] };
} catch (e) { return { content: [{ type: 'text', text: 'Error: ' + e.message }] }; }
    }
    
    async start() {
        try {
            const transport = new StdioServerTransport();
            await this.server.connect(transport);
            console.error('Writeups MCP Server started. Waiting for requests...');
            
            process.on('SIGINT', () => {
                console.error('Shutting down Writeups MCP Server...');
                process.exit(0);
            });
        } catch (error) {
            console.error('Failed to start MCP Writeups Server:', error);
            process.exit(1);
        }
    }
}

const server = new WriteupsMCPServer();
server.start().catch(console.error);