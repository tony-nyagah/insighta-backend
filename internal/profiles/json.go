package profiles

import (
	"encoding/json"
	"io"
	"net/http"
)

func decodeJSON(r *http.Request, v interface{}) error {
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
}

func newJSONEncoder(w io.Writer) *json.Encoder {
	return json.NewEncoder(w)
}
