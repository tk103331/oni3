package a2a

import (
	"encoding/json"
	"fmt"
	"strings"
)

// JSON-RPC 2.0 method names used by A2A 1.0.
const (
	MethodSendMessage          = "SendMessage"
	MethodSendStreamingMessage = "SendStreamingMessage"
	MethodGetTask              = "GetTask"
	MethodListTasks            = "ListTasks"
	MethodCancelTask           = "CancelTask"
	MethodSubscribeToTask      = "SubscribeToTask"
)

// JSON-RPC 2.0 error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// Request is one JSON-RPC request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is one JSON-RPC response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is a JSON-RPC error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func rpcError(code int, message string) *RPCError {
	return &RPCError{Code: code, Message: message}
}

func success(id json.RawMessage, result any) Response {
	return Response{JSONRPC: "2.0", ID: id, Result: result}
}

func failure(id json.RawMessage, err *RPCError) Response {
	if id == nil {
		id = json.RawMessage("null")
	}
	return Response{JSONRPC: "2.0", ID: id, Error: err}
}

// CanonicalMethod maps supported 0.3 slash names to their A2A 1.0 names.
func CanonicalMethod(method string) string {
	switch strings.TrimSpace(method) {
	case MethodSendMessage, "message/send":
		return MethodSendMessage
	case MethodSendStreamingMessage, "message/stream":
		return MethodSendStreamingMessage
	case MethodGetTask, "tasks/get":
		return MethodGetTask
	case MethodListTasks, "tasks/list":
		return MethodListTasks
	case MethodCancelTask, "tasks/cancel":
		return MethodCancelTask
	case MethodSubscribeToTask, "tasks/resubscribe":
		return MethodSubscribeToTask
	default:
		return ""
	}
}

func parseRequest(body []byte) (Request, *RPCError) {
	var request Request
	if err := json.Unmarshal(body, &request); err != nil {
		return Request{}, rpcError(CodeParseError, "invalid JSON: "+err.Error())
	}
	if request.JSONRPC != "2.0" {
		return request, rpcError(CodeInvalidRequest, "jsonrpc must be \"2.0\"")
	}
	if len(request.ID) == 0 || string(request.ID) == "null" {
		return request, rpcError(CodeInvalidRequest, "id is required")
	}
	if CanonicalMethod(request.Method) == "" {
		return request, rpcError(CodeMethodNotFound, fmt.Sprintf("method not found: %s", request.Method))
	}
	return request, nil
}
