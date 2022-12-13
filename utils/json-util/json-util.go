package json_util

import (
	"bytes"
	"encoding/json"
	"strings"
)

func Format(v interface{}) string {
	buf := bytes.NewBuffer(nil)
	e := json.NewEncoder(buf)
	e.SetEscapeHTML(false)
	e.SetIndent("", " ")
	err := e.Encode(v)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(buf.String())
}

func ToString(v interface{}) string {
	return string(ToBin(v))
}

func ToBin(v interface{}) []byte {
	buf := bytes.NewBuffer(nil)
	e := json.NewEncoder(buf)
	e.SetEscapeHTML(false)
	err := e.Encode(v)
	if err != nil {
		return nil
	}
	return bytes.TrimSpace(buf.Bytes())
}
