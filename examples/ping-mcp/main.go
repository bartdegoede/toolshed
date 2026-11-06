// ping-mcp lets your agent send push notifications to your phone through
// ntfy (https://ntfy.sh): "run the migration and ping me when it's done".
//
// It is also toolshed's example server: small enough to read in one sitting
// and a good template for your own. Configuration comes from environment
// variables, which toolshed fills in at startup:
//
//	NTFY_TOPIC   the topic to publish to (a secret: anyone who knows it can
//	             message you, so toolshed keeps it in the OS keyring)
//	NTFY_SERVER  optional, for self-hosted ntfy (default https://ntfy.sh)
//	NTFY_TOKEN   optional access token for protected topics
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var version = "dev"

type notifyArgs struct {
	Message  string   `json:"message" jsonschema:"the notification text"`
	Title    string   `json:"title,omitempty" jsonschema:"optional short title"`
	Priority string   `json:"priority,omitempty" jsonschema:"min, low, default, high or urgent"`
	Tags     []string `json:"tags,omitempty" jsonschema:"optional emoji shortcodes such as white_check_mark or warning"`
}

func main() {
	if err := newServer().Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("ping-mcp: %v", err)
	}
}

func newServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "ping-mcp", Version: version}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name: "notify",
		Description: "Send a push notification to the user's phone. Use it when a long task " +
			"finishes, fails, or needs their input while they're away from the screen.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: ptr(true), DestructiveHint: ptr(false)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args notifyArgs) (*mcp.CallToolResult, any, error) {
		if err := notify(ctx, args); err != nil {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil, nil
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "Notification sent."}}}, nil, nil
	})
	return server
}

func notify(ctx context.Context, args notifyArgs) error {
	topic := os.Getenv("NTFY_TOPIC")
	if topic == "" {
		return fmt.Errorf("NTFY_TOPIC is not set: run `toolshed install ping` to configure it")
	}
	if strings.TrimSpace(args.Message) == "" {
		return fmt.Errorf("message is empty")
	}
	server := strings.TrimRight(os.Getenv("NTFY_SERVER"), "/")
	if server == "" {
		server = "https://ntfy.sh"
	}

	body, _ := json.Marshal(map[string]any{
		"topic": topic, "message": args.Message, "title": args.Title,
		"priority": priority(args.Priority), "tags": args.Tags,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if tok := os.Getenv("NTFY_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("sending notification: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("ntfy returned %s", resp.Status)
	}
	return nil
}

// priority maps ntfy's names to its 1–5 scale; unknown values mean default.
func priority(p string) int {
	switch strings.ToLower(p) {
	case "min":
		return 1
	case "low":
		return 2
	case "high":
		return 4
	case "urgent", "max":
		return 5
	}
	return 3
}

func ptr[T any](v T) *T { return &v }
