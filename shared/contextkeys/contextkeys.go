package contextkeys

type CtxKey string

const (
	TraceIDKey     CtxKey = "trace_id"
	MethodKey      CtxKey = "method"
	PathKey        CtxKey = "path"
	RemoteIPKey    CtxKey = "remote_ip"
	RequestTimeKey CtxKey = "duration"
	StatusCodeKey  CtxKey = "status_code"
	PackageName    CtxKey = "package"
	RequestData    CtxKey = "request_data"
)
