package logger

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/aliyun/aliyun-log-go-sdk/producer"
	"github.com/peterq/fancy-go/utils/app"
	"github.com/pkg/errors"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

var aliLog = &aliLogger{}

func AliConfig() AliLogConfig {
	return aliLog.config
}

func GetAliLogProducer() *producer.Producer {
	return aliLog.producer
}

func InitAliLog(c AliLogConfig) *producer.Producer {
	if c.NotifyProject == "" {
		c.NotifyProject = c.Project
	}

	if c.NotifyStore == "" {
		c.NotifyStore = "notify"
	}

	aliLog.initOnce.Do(func() {
		hostname, err := os.Hostname()
		if err != nil {
			panic(err)
		}
		c.Host = hostname
		aliLog.config = c
		producerConfig := producer.GetDefaultProducerConfig()
		producerConfig.Endpoint = c.Endpoint
		producerConfig.AccessKeyID = c.AccessKeyId
		producerConfig.AccessKeySecret = c.AccessKeySecret
		aliLog.producer = producer.InitProducer(producerConfig)
		aliLog.producer.Start()
		Default.Info("ali log start", c)
		app.OnExit(func(ctx context.Context) {
			err := app.GetExitErr()
			if err != nil {
				Default.WithStage("app-exit-with-error").Error(err)
			} else {
				Default.WithStage("app-exit").Info("app exit")
			}
			Default.Info("ali log closing")
			time.Sleep(time.Millisecond * 100) // 其他任务退出前可能需要打印日志, 休眠100ms让其他任务可以继续打印日志
			log.Println("closing ali log")
			aliLog.producer.SafeClose()
			log.Println("ali log safely closed")
		})
	})
	return aliLog.producer
}

type AliLogConfig struct {
	Endpoint        string
	AccessKeyId     string
	AccessKeySecret string
	Project         string
	LogStore        string
	NotifyProject   string
	NotifyStore     string

	Enviroment string
	Host       string
	Service    string
}

type aliLogger struct {
	initOnce sync.Once
	producer *producer.Producer
	config   AliLogConfig
}

type groupHook struct {
	group string
}

var AliLogHook func(map[string]string) map[string]string

func (h *groupHook) Fire(record *Record) {
	if aliLog.producer == nil {
		return
	}
	var fields = map[string]string{
		"content":     record.Message,
		"environment": aliLog.config.Enviroment,
		"host":        aliLog.config.Host,
		"level":       record.Level.String(),
		"service":     aliLog.config.Service,
		"location":    record.Location,
	}
	for k, v := range record.Fields {
		if _, ok := fields[k]; ok {
			fields["app_"+k] = fmt.Sprint(v)
		} else {
			fields[k] = fmt.Sprint(v)
		}
	}

	if len(record.Data) > 0 {
		bin, err := json.Marshal(record.Data)
		if err != nil {
			bin, _ = json.Marshal(map[string]string{"raw": fmt.Sprintf("%#v", record.Data)})
		}
		fields["data"] = string(bin)
	}

	if AliLogHook != nil {
		fields = AliLogHook(fields)
	}

	if h.group == "notify" {
		aliLog.producer.SendLog(aliLog.config.NotifyProject, aliLog.config.NotifyStore, "",
			"utils/logger", producer.GenerateLog(uint32(time.Now().Unix()), fields))
	}

	aliLog.producer.SendLog(aliLog.config.Project, aliLog.config.LogStore, "",
		"utils/logger", producer.GenerateLog(uint32(time.Now().Unix()), fields))
	return
}

func AliLogFromStr(c string) (AliLogConfig, error) {
	var logConfig AliLogConfig
	parts := strings.Split(c, "&")
	mp := map[string]string{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.Split(part, "=")
		if len(kv) != 2 {
			return logConfig, errors.New(fmt.Sprintf("%s invalid kv pair", kv))
		}
		mp[kv[0]] = kv[1]
	}
	for _, k := range []string{"ep", "ak", "sk", "proj", "store", "service"} {
		if mp[k] == "" {
			return logConfig, errors.New(fmt.Sprintf("%s must be set", k))
		}
	}
	return AliLogConfig{
		Endpoint:        mp["ep"],
		AccessKeyId:     mp["ak"],
		AccessKeySecret: mp["sk"],
		Project:         mp["proj"],
		LogStore:        mp["store"],
		NotifyProject:   mp["notifyProj"],
		NotifyStore:     mp["notifyStore"],
		Enviroment:      mp["env"],
		Host:            mp["host"],
		Service:         mp["service"],
	}, nil
}
