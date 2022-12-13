package curl_to_go

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/mattn/go-shellwords"
	"github.com/pkg/errors"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type RequestOption struct {
	Do        func(req *http.Request) (*http.Response, error)
	Resp      *http.Response
	Header    map[string]string
	JsonSend  interface{}
	JsonRet   interface{}
	StringRet *string
}

type RequestFactory func(opt *RequestOption) error

func CurlToGo(curlCmd string) (RequestFactory, error) {
	args, err := shellwords.Parse(curlCmd)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse curl command")
	}
	var method string
	var link string
	var header = map[string]string{}
	var boolOpt = map[string]bool{
		"compressed": false,
	}
	handleArg := func(name, value string) error {
		var argError error
		switch name {
		case "X":
			method = value
		case "H":
			parts := strings.SplitN(value, ":", 2)
			if len(parts) != 2 {
				return errors.Errorf("invalid header: %s", value)
			}
			header[parts[0]], argError = url.QueryUnescape(strings.TrimSpace(parts[1]))
			if err != nil {
				return errors.Wrap(err, "failed to unescape header value: "+value)
			}
		default:
			argError = errors.Errorf("unknown arg: (%s)", name)
		}
		return argError
	}
	args = args[1:]
	// remove space
	for i := 0; i < len(args); i++ {
		if strings.TrimSpace(args[i]) == "" {
			args = append(args[:i], args[i+1:]...)
			i--
		}
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			arg = strings.TrimLeft(arg, "--")
			if _, ok := boolOpt[arg]; ok {
				boolOpt[arg] = true
				continue
			}
			if i == len(args)-1 || strings.HasPrefix(args[i+1], "-") {
				return nil, errors.New("option without value: " + arg)
			}
			i++
			if err = handleArg(arg, args[i]); err != nil {
				return nil, errors.Wrap(err, "failed to handle arg")
			}
			continue
		}
		if link != "" {
			return nil, errors.New(fmt.Sprintf("link cant be set to (%s), already set to (%s)", arg, link))
		}
		link = arg
	}

	requestTpl, err := http.NewRequest(method, link, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create request template")
	}

	return factoryFromTpl(requestTpl), nil
}

func factoryFromTpl(requestTpl *http.Request) RequestFactory {
	return func(opt *RequestOption) error {
		if opt == nil {
			opt = &RequestOption{}
		}
		var body io.Reader
		if opt.JsonSend != nil {
			data, err := json.Marshal(opt.JsonSend)
			if err != nil {
				return errors.Wrap(err, "failed to marshal json")
			}
			body = bytes.NewReader(data)
		}
		req, err := http.NewRequest(requestTpl.Method, requestTpl.URL.String(), body)
		if err != nil {
			return errors.Wrap(err, "failed to create request")
		}
		req.Header = requestTpl.Header

		if opt.Header != nil {
			for k, v := range opt.Header {
				req.Header.Set(k, v)
			}
		}

		var do = http.DefaultClient.Do
		if opt.Do != nil {
			do = opt.Do
		}
		opt.Resp, err = do(req)
		if err != nil {
			return errors.Wrap(err, "failed to do request")
		}
		var bodyBytes []byte
		if opt.StringRet != nil || opt.JsonRet != nil {
			defer opt.Resp.Body.Close()
			bodyBytes, err = io.ReadAll(opt.Resp.Body)
			if err != nil {
				return errors.Wrap(err, "failed to read response body")
			}
		}
		if opt.StringRet != nil {
			*opt.StringRet = string(bodyBytes)
		}
		if opt.JsonRet != nil {
			err = json.Unmarshal(bodyBytes, opt.JsonRet)
			if err != nil {
				return errors.Wrap(err, "failed to unmarshal json")
			}
		}
		return nil
	}
}
