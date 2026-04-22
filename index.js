#!/usr/bin/env node
/**
 * Writeups MCP Server
 * 
 * MCP server for CTF writeups knowledge base search
 * Provides tools to search through indexed writeups for techniques, vulnerabilities, and solutions
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
        this.cache = new Map(); // Cache for recent searches
        this.cacheId = 1;
        
        this.server = new Server({
            name: 'Writeups MCP Server',
            version: '1.0.0'
        }, {
            capabilities: {
                tools: {}
            }
        });
        
        this.server.setRequestHandler(ListToolsRequestSchema, async () => {
            return {
                tools: [
                    {
                        name: 'search_writeups',
                        description: 'Search CTF writeups knowledge base - returns numbered results. Use read_writeup with ID to read full content.',
                        inputSchema: {
                            type: 'object',
                            properties: {
                                query: {
                                    type: 'string',
                                    description: 'Search query (keywords, technique name, CVE, etc.)'
                                },
                                limit: {
                                    type: 'number',
                                    description: 'Maximum number of results (default: 10)',
                                    default: 10
                                }
                            },
                            required: ['query']
                        }
                    },
                    {
                        name: 'read_writeup',
                        description: 'Read a specific writeup by ID (from search results) or path. Shows lines around match or specified range.',
                        inputSchema: {
                            type: 'object',
                            properties: {
                                id: {
                                    type: 'number',
                                    description: 'Writeup ID from search results'
                                },
                                path: {
                                    type: 'string',
                                    description: 'Full path to writeup file (alternative to ID)'
                                },
                                lines: {
                                    type: 'string',
                                    description: 'Line range to show, e.g. "100-150" or just "100" for around that line'
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
                return await this.search(args.query, args.limit || 10);
            } else if (name === 'read_writeup') {
                return await this.readWriteup(args.id, args.path, args.lines);
            }
            
            return { content: [{ type: 'text', text: `Unknown tool: ${name}` }] };
        });
    }
    
    search(query, limit) {
        try {
            const db = new Database(DB_PATH, { readonly: true });
            
            // First get matching docs with their rowids
            const stmt = db.prepare(`
                SELECT d.rowid as id, d.title, d.path
                FROM docs d
                JOIN docs_fts f ON d.rowid = f.rowid
                WHERE docs_fts MATCH ?
                LIMIT ?
            `);
            
            const rows = stmt.all(query, limit);
            
            // Now get snippet with line numbers using bm25 ranking
            const results = [];
            for (let i = 0; i < rows.length; i++) {
                const row = rows[i];
                const snippetStmt = db.prepare(`
                    SELECT snippet(docs_fts, -1, '===', '===', '...', 10) as snippet
                    FROM docs_fts WHERE rowid = ?
                `);
                const snippetRow = snippetStmt.get(row.id);
                
                // Determine actual line range from snippet
                let lineInfo = "";
                if (snippetRow && snippetRow.snippet) {
                    // Count newlines before snippet to estimate position
                    const fullStmt = db.prepare(`SELECT content FROM docs_fts WHERE rowid = ?`);
                    const fullRow = fullStmt.get(row.id);
                    if (fullRow && fullRow.content) {
                        const matchPos = fullRow.content.indexOf(snippetRow.snippet.substring(10, 50));
                        if (matchPos >= 0) {
                            const beforeMatch = fullRow.content.substring(0, matchPos);
                            const lineNum = (beforeMatch.match(/\n/g) || []).length + 1;
                            lineInfo = ` (lines ~${lineNum}-${lineNum + 10})`;
                        }
                    }
                }
                
                const resultId = this.cacheId++;
                this.cache.set(resultId, { path: row.path, db });
                
                results.push({
                    type: 'text',
                    text: `[${resultId}] ${row.title}${lineInfo}\nPath: ${row.path}\n---snippet---\n${snippetRow?.snippet || 'No preview'}\n`
                });
            }
            
            db.close();
            return { content: results };
        } catch (error) {
            return { content: [{ type: 'text', text: `Error: ${error.message}` }] };
        }
    }
    
    readWriteup(id, pathArg, linesArg) {
        try {
            let filePath;
            
            if (id && this.cache.has(id)) {
                filePath = this.cache.get(id).path;
            } else if (pathArg) {
                filePath = pathArg;
            } else {
                return { content: [{ type: 'text', text: 'Error: Must provide ID from search or path' }] };
            }
            
            if (!fs.existsSync(filePath)) {
                return { content: [{ type: 'text', text: `File not found: ${filePath}` }] };
            }
            
            let content = fs.readFileSync(filePath, 'utf-8');
            let lines = content.split('\n');
            let range = '';
            let startLine = 1;
            let endLine = lines.length;
            
            if (linesArg) {
                if (linesArg.includes('-')) {
                    const [s, e] = linesArg.split('-').map(Number);
                    startLine = s || 1;
                    endLine = e || lines.length;
                } else {
                    const lineNum = parseInt(linesArg);
                    startLine = Math.max(1, lineNum - 10);
                    endLine = lineNum + 10;
                }
                range = ` (lines ${startLine}-${endLine})`;
            }
            
            const selectedLines = lines.slice(startLine - 1, endLine);
            const display = selectedLines.map((l, i) => `${startLine + i}: ${l}`).join('\n');
            
            return {
                content: [{
                    type: 'text',
                    text: `=== ${path.basename(filePath)}${range} ===\n\n${display}\n\n---end---`
                }]
            };
        } catch (error) {
            return { content: [{ type: 'text', text: `Error: ${error.message}` }] };
        }
    }
    
    async start() {
        try {
            const transport = new StdioServerTransport();
            await this.server.connect(transport);
            console.error("Writeups MCP Server started. Waiting for requests...");
            
            process.on('SIGINT', () => {
                console.error("Shutting down Writeups MCP Server...");
                process.exit(0);
            });
        } catch (error) {
            console.error("Failed to start MCP Writeups Server:", error);
            process.exit(1);
        }
    }
}

const server = new WriteupsMCPServer();
server.start().catch(console.error);