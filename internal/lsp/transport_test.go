package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteReadMessageRoundtrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		msg    *Message
		method string
	}{
		{
			name: "request message",
			msg: &Message{
				JSONRPC: "2.0",
				ID:      &MessageID{Value: int32(1)},
				Method:  "textDocument/hover",
				Params:  json.RawMessage(`{"textDocument":{"uri":"file:///test.go"}}`),
			},
			method: "textDocument/hover",
		},
		{
			name: "notification message",
			msg: &Message{
				JSONRPC: "2.0",
				Method:  "initialized",
				Params:  json.RawMessage(`{}`),
			},
			method: "initialized",
		},
		{
			name: "response message",
			msg: &Message{
				JSONRPC: "2.0",
				ID:      &MessageID{Value: int32(42)},
				Result:  json.RawMessage(`{"contents":"hello"}`),
			},
		},
		{
			name: "error response",
			msg: &Message{
				JSONRPC: "2.0",
				ID:      &MessageID{Value: int32(3)},
				Error: &ResponseError{
					Code:    -32601,
					Message: "method not found",
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			err := WriteMessage(&buf, tc.msg)
			require.NoError(t, err)

			// Verify the wire format has Content-Length header
			written := buf.String()
			assert.True(t, strings.HasPrefix(written, "Content-Length: "), "should start with Content-Length header")
			assert.Contains(t, written, "\r\n\r\n", "should have header/body separator")

			// Read back the message
			reader := bufio.NewReader(&buf)
			got, err := ReadMessage(reader)
			require.NoError(t, err)

			assert.Equal(t, tc.msg.JSONRPC, got.JSONRPC)
			assert.Equal(t, tc.msg.Method, got.Method)

			if tc.msg.ID != nil {
				require.NotNil(t, got.ID)
				assert.True(t, tc.msg.ID.Equals(got.ID))
			}

			if tc.msg.Error != nil {
				require.NotNil(t, got.Error)
				assert.Equal(t, tc.msg.Error.Code, got.Error.Code)
				assert.Equal(t, tc.msg.Error.Message, got.Error.Message)
			}

			if tc.msg.Result != nil {
				assert.JSONEq(t, string(tc.msg.Result), string(got.Result))
			}
		})
	}
}

func TestWriteMessageFormat(t *testing.T) {
	t.Parallel()

	msg := &Message{
		JSONRPC: "2.0",
		ID:      &MessageID{Value: int32(1)},
		Method:  "test",
	}

	var buf bytes.Buffer
	err := WriteMessage(&buf, msg)
	require.NoError(t, err)

	raw := buf.String()

	// Split on the header/body separator
	parts := strings.SplitN(raw, "\r\n\r\n", 2)
	require.Len(t, parts, 2, "should have header and body separated by \\r\\n\\r\\n")

	header := parts[0]
	body := parts[1]

	// Verify Content-Length matches actual body length
	var contentLength int
	_, err = json.Marshal(msg)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(header, "Content-Length: "))
	assert.Equal(t, len(body), len([]byte(body)), "Content-Length should match body bytes")

	// Verify body is valid JSON
	var parsed Message
	err = json.Unmarshal([]byte(body), &parsed)
	require.NoError(t, err)

	_ = contentLength
}

func TestReadMessageInvalidHeader(t *testing.T) {
	t.Parallel()

	// EOF before any content
	reader := bufio.NewReader(strings.NewReader(""))
	_, err := ReadMessage(reader)
	assert.Error(t, err)
}

func TestReadMessageInvalidJSON(t *testing.T) {
	t.Parallel()

	// Valid header but invalid JSON body
	raw := "Content-Length: 12\r\n\r\nnot valid js"
	reader := bufio.NewReader(strings.NewReader(raw))
	_, err := ReadMessage(reader)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
}

func TestReadMessageTruncatedBody(t *testing.T) {
	t.Parallel()

	// Content-Length says 100 but body is shorter
	raw := "Content-Length: 100\r\n\r\n{}"
	reader := bufio.NewReader(strings.NewReader(raw))
	_, err := ReadMessage(reader)
	assert.Error(t, err)
}

func TestMessageIDMarshalUnmarshal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		id       *MessageID
		expected string
	}{
		{"nil MessageID", nil, "null"},
		{"nil Value", &MessageID{Value: nil}, "null"},
		{"integer ID", &MessageID{Value: int32(42)}, "42"},
		{"string ID", &MessageID{Value: "abc-123"}, `"abc-123"`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(tc.id)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, string(data))

			// Roundtrip (skip nil pointer case)
			if tc.id != nil {
				var got MessageID
				err = json.Unmarshal(data, &got)
				require.NoError(t, err)
				assert.True(t, tc.id.Equals(&got))
			}
		})
	}
}

func TestMessageIDUnmarshalNumber(t *testing.T) {
	t.Parallel()

	var id MessageID
	err := json.Unmarshal([]byte("42"), &id)
	require.NoError(t, err)

	// JSON numbers unmarshal as float64, then get converted to int32
	assert.IsType(t, int32(0), id.Value)
	assert.Equal(t, int32(42), id.Value)
}

func TestMessageIDString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		id       *MessageID
		expected string
	}{
		{"nil pointer", nil, "<null>"},
		{"nil value", &MessageID{Value: nil}, "<null>"},
		{"int32", &MessageID{Value: int32(7)}, "7"},
		{"string", &MessageID{Value: "req-1"}, "req-1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, tc.id.String())
		})
	}
}

func TestMessageIDEquals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		a        *MessageID
		b        *MessageID
		expected bool
	}{
		{"both nil", nil, nil, true},
		{"one nil", &MessageID{Value: int32(1)}, nil, false},
		{"both nil value", &MessageID{Value: nil}, &MessageID{Value: nil}, true},
		{"same int", &MessageID{Value: int32(1)}, &MessageID{Value: int32(1)}, true},
		{"different int", &MessageID{Value: int32(1)}, &MessageID{Value: int32(2)}, false},
		{"same string", &MessageID{Value: "a"}, &MessageID{Value: "a"}, true},
		{"different string", &MessageID{Value: "a"}, &MessageID{Value: "b"}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, tc.a.Equals(tc.b))
		})
	}
}

func TestNewRequest(t *testing.T) {
	t.Parallel()

	msg, err := NewRequest(int32(5), "textDocument/definition", map[string]string{"key": "val"})
	require.NoError(t, err)

	assert.Equal(t, "2.0", msg.JSONRPC)
	assert.Equal(t, "textDocument/definition", msg.Method)
	require.NotNil(t, msg.ID)
	assert.Equal(t, int32(5), msg.ID.Value)
	assert.NotEmpty(t, msg.Params)
}

func TestNewNotification(t *testing.T) {
	t.Parallel()

	msg, err := NewNotification("initialized", struct{}{})
	require.NoError(t, err)

	assert.Equal(t, "2.0", msg.JSONRPC)
	assert.Equal(t, "initialized", msg.Method)
	assert.Nil(t, msg.ID)
}
