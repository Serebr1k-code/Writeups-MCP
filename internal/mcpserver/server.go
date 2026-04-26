package mcpserver

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/Serebr1k-code/Writeups-MCP/internal/writeups"
)

func New(repo *writeups.Repository) *server.MCPServer {
	s := server.NewMCPServer(
		"writeups-mcp",
		"1.0.0",
		server.WithToolCapabilities(false),
		server.WithRecovery(),
	)

	searchTool := mcp.NewTool(
		"search_writeups",
		mcp.WithDescription("Search the writeups knowledge base and return numbered results for reading."),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search query")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of results"), mcp.DefaultNumber(10), mcp.Min(1), mcp.Max(100)),
	)

	s.AddTool(searchTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query, err := request.RequireString("query")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		results, err := repo.Search(query, request.GetInt("limit", 10))
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return mcp.NewToolResultText(writeups.FormatSearchResults(query, results)), nil
	})

	readTool := mcp.NewTool(
		"read_writeup",
		mcp.WithDescription("Read a writeup by search result id or direct file path, optionally with a line range."),
		mcp.WithNumber("id", mcp.Description("Writeup id from search results"), mcp.Min(1)),
		mcp.WithString("path", mcp.Description("Direct file path")),
		mcp.WithString("lines", mcp.Description("Line selector: 100-150, 50, 100-")),
	)

	s.AddTool(readTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := repo.Read(request.GetInt("id", 0), request.GetString("path", ""), request.GetString("lines", ""))
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return mcp.NewToolResultText(result.Text), nil
	})

	helpTool := mcp.NewTool(
		"help",
		mcp.WithDescription("Show usage help for writeups MCP tools."),
		mcp.WithString("tool", mcp.Description("search, read, or all")),
	)

	s.AddTool(helpTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText(writeups.HelpText(request.GetString("tool", "all"))), nil
	})

	return s
}
