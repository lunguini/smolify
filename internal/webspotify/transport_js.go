//go:build js && wasm

package webspotify

import (
	"context"
	"errors"
	"fmt"
	"syscall/js"
)

const maxFetchBytes = 8 << 20

type promiseResult struct {
	value js.Value
	err   error
}

// Do sends requests through the browser's fetch API. This is used for both
// the Web API and cover-image loads in the wasm host.
func (FetchTransport) Do(ctx context.Context, request Request) (Response, error) {
	global := js.Global()
	fetch := global.Get("fetch")
	if fetch.IsUndefined() || fetch.IsNull() {
		return Response{}, errors.New("browser fetch is unavailable")
	}
	opts := global.Get("Object").New()
	opts.Set("method", request.Method)
	headers := global.Get("Object").New()
	for key, value := range request.Header {
		headers.Set(key, value)
	}
	opts.Set("headers", headers)
	var controller js.Value
	if ctor := global.Get("AbortController"); !ctor.IsUndefined() && !ctor.IsNull() {
		controller = ctor.New()
		opts.Set("signal", controller.Get("signal"))
	}
	if len(request.Body) > 0 {
		opts.Set("body", string(request.Body))
	}
	promise := fetch.Invoke(request.URL, opts)
	response, err := await(ctx, promise)
	if err != nil {
		if !controller.IsUndefined() && !controller.IsNull() {
			controller.Call("abort")
		}
		return Response{}, err
	}
	status := response.Get("status").Int()
	buffer, err := await(ctx, response.Call("arrayBuffer"))
	if err != nil {
		return Response{}, err
	}
	bytes := global.Get("Uint8Array").New(buffer)
	length := bytes.Get("byteLength").Int()
	if length > maxFetchBytes {
		return Response{}, fmt.Errorf("browser fetch response too large: %d bytes", length)
	}
	body := make([]byte, length)
	js.CopyBytesToGo(body, bytes)
	responseHeaders := response.Get("headers")
	header := map[string]string{}
	for _, name := range []string{"retry-after", "content-type"} {
		value := responseHeaders.Call("get", name)
		if !value.IsNull() && !value.IsUndefined() {
			header[name] = value.String()
		}
	}
	return Response{Status: status, Header: header, Body: body}, nil
}

func await(ctx context.Context, promise js.Value) (js.Value, error) {
	settled := make(chan promiseResult, 1)
	resolve := js.FuncOf(func(_ js.Value, args []js.Value) any {
		value := js.Undefined()
		if len(args) > 0 {
			value = args[0]
		}
		settled <- promiseResult{value: value}
		return nil
	})
	reject := js.FuncOf(func(_ js.Value, args []js.Value) any {
		message := "browser promise rejected"
		if len(args) > 0 {
			value := args[0]
			if value.Type() == js.TypeObject {
				if text := value.Get("message"); !text.IsUndefined() && !text.IsNull() {
					message = text.String()
				} else {
					message = value.String()
				}
			} else {
				message = value.String()
			}
		}
		settled <- promiseResult{err: errors.New(message)}
		return nil
	})
	promise.Call("then", resolve, reject)
	select {
	case result := <-settled:
		resolve.Release()
		reject.Release()
		return result.value, result.err
	case <-ctx.Done():
		go func() {
			<-settled
			resolve.Release()
			reject.Release()
		}()
		return js.Undefined(), ctx.Err()
	}
}
