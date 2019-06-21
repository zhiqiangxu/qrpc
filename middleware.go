package qrpc

// HandlerWithMW wraps a handle with middleware
func HandlerWithMW(handler Handler, middleware ...MiddlewareFunc) Handler {
	if len(middleware) == 0 {
		return handler
	}

	return &handlerMW{handler: handler, mw: middleware}
}

type handlerMW struct {
	mw      []MiddlewareFunc
	handler Handler
}

func (h *handlerMW) ServeQRPC(w FrameWriter, r *RequestFrame) {
	for _, middleware := range h.mw {
		if !middleware(w, r) {
			return
		}
	}

	h.handler.ServeQRPC(w, r)
}
