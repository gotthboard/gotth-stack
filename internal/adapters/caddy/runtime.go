package caddy

import (
	"context"
	"io"
	"net/http"
)

func (reader *adminReader) Read(ctx context.Context) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, reader.url, nil)
	if err != nil {
		return nil, ErrRuntime
	}
	request.Header.Set("Accept", "application/json")
	response, err := reader.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrRuntime
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength > MaxJSONBytes {
		return nil, ErrRuntime
	}
	value, err := io.ReadAll(io.LimitReader(response.Body, MaxJSONBytes+1))
	if err != nil {
		return nil, ErrRuntime
	}
	if len(value) > MaxJSONBytes {
		return nil, ErrLimit
	}
	return canonicalJSON(value)
}
