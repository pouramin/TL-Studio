package main

import (
    "fmt"
    "strings"
)

// A final checkpoint must not resend a long raw Agent conversation. Long
// histories can exceed even a model's largest context budget. Flattening the
// most recent outcomes also avoids orphaned tool-call IDs in the wire protocol.
func nativeCheckpointClip(text string, maxRunes int) string {
    r := []rune(strings.TrimSpace(text))
    if len(r) <= maxRunes { return string(r) }
    return string(r[:maxRunes]) + " …[truncated]"
}

func nativeCheckpointConversation(history []nativeConversationMessage, originalRequest string) []nativeConversationMessage {
    var b strings.Builder
    b.WriteString("Original user request: ")
    b.WriteString(nativeCheckpointClip(originalRequest, 750))
    b.WriteString("\nRecent verified conversation/tool record (earlier turns omitted):\n")
    start := len(history) - 12
    if start < 0 { start = 0 }
    for _, m := range history[start:] {
        switch m.Role {
        case "tool":
            b.WriteString(fmt.Sprintf("- Tool result %s: %s\n",
                nativeCheckpointClip(m.ToolName, 80), nativeCheckpointClip(m.Text, 280)))
        case "assistant":
            if strings.TrimSpace(m.Text) != "" {
                b.WriteString("- Assistant: ")
                b.WriteString(nativeCheckpointClip(m.Text, 280))
                b.WriteByte('\n')
            }
            for _, call := range m.ToolCalls {
                b.WriteString("- Tool requested: ")
                b.WriteString(nativeCheckpointClip(call.Name, 80))
                b.WriteByte('\n')
            }
        case "user":
            b.WriteString("- User: ")
            b.WriteString(nativeCheckpointClip(m.Text, 280))
            b.WriteByte('\n')
        }
    }
    // Absolute upper bound in runes, independent of provider, accumulated
    // tool output size, or malformed UTF-8 returned by a tool.
    return []nativeConversationMessage{{Role:"user", Text: nativeCheckpointClip(b.String(), 5500)}}
}
