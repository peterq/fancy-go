package logger

import (
	"encoding/json"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
	"time"
)

func TestInitAliLog(t *testing.T) {
	Default.Debug("before init")
	c := AliLogConfig{
		Enviroment: "test-case",
		Host:       "test-case",
		Service:    "test-case",
	}
	err := json.Unmarshal([]byte(os.Getenv("sls_conf")), &c)
	if !assert.NoError(t, err) {
		return
	}
	InitAliLog(c)
	Default.Debug("after init")
	Logger("test-logger").WithStage("test-case").Error(errors.Wrap(errors.New("xx"), "xxx"))
	time.Sleep(time.Second * 3)
}
