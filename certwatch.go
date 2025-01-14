package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type Config struct {
	RedisUrl    string
	KeyPrefix   string
	ValuePrefix string
	AcmeDirName string

	CertDir   string
	Certs     []string
	Cmd       string
	Debug     bool
	SleepTime time.Duration
}

var (
	config Config
	client *redis.Client
)

func main() {
	flag.StringVar(&config.RedisUrl, "redisurl", "", "URL for redis instance")
	flag.StringVar(&config.KeyPrefix, "keyprefix", "caddy", "prefix for keys")
	flag.StringVar(&config.ValuePrefix, "valueprefix", "caddy-storage-redis", "prefix for values")
	flag.StringVar(&config.AcmeDirName, "acmedir", "acme-v02.api.letsencrypt.org-directory", "subdir for ACME")
	flag.StringVar(&config.CertDir, "certdir", "/var/lib/certwatch", "directory for storing certificates locally")
	flag.StringVar(&config.Cmd, "cmd", "", "command to execute if certificates have been changed")
	flag.BoolVar(&config.Debug, "debug", false, "verbose debug output")
	flag.DurationVar(&config.SleepTime, "sleep", 10*time.Second, "sleep duration after error")
	flag.Parse()
	config.Certs = flag.Args()
	level := new(slog.LevelVar) // Info by default
	if config.Debug {
		level.Set(slog.LevelDebug)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	}))
	slog.SetDefault(logger)
	slog.Debug("config", "config", config)
	if len(config.RedisUrl) == 0 || len(config.Certs) == 0 {
		flag.Usage()
		os.Exit(1)
	}
	err := os.MkdirAll(config.CertDir, 0700)
	if err != nil {
		slog.Error("MkdirAll", "err", err)
		os.Exit(1)
	}
	opt, err := redis.ParseURL(config.RedisUrl)
	if err != nil {
		slog.Error("redis.ParseURL", "err", err)
		os.Exit(1)
	}
	client = redis.NewClient(opt)
	ctx := context.Background()
	for {
		slog.Info("listening for cert changes")
		err = listenRedis(ctx)
		if err != nil {
			slog.Error("listenRedis", "err", err)
		}
		slog.Info("sleep after redis error", "dur", config.SleepTime)
		time.Sleep(config.SleepTime)
	}
}

func listenRedis(ctx context.Context) error {
	needExec := false
	for _, i := range config.Certs {
		didOne, err := handleCert(ctx, i)
		if err != nil {
			return err
		}
		if didOne {
			needExec = true
		}
	}
	if needExec {
		if len(config.Cmd) > 0 {
			slog.Info("exec", "cmd", config.Cmd)
			cmd := exec.Command("sh", "-c", config.Cmd)
			outerr, err := cmd.CombinedOutput()
			if err != nil {
				slog.Error("exec", "err", err, "outerr", string(outerr))
			}
		}
	}
	keypath := "__keyspace@0__:" + config.KeyPrefix + "/certificates/" + config.AcmeDirName + "/"
	pubsub := client.PSubscribe(ctx, keypath+"*")
	defer pubsub.Close()
	for {
		msg, err := pubsub.ReceiveMessage(ctx)
		if err != nil {
			return err
		}
		needExec := false
		key := strings.TrimPrefix(msg.Channel, keypath)
		slog.Debug("msg", "key", key, "payload", msg.Payload)
		for _, i := range config.Certs {
			if strings.HasPrefix(key, i) {
				switch msg.Payload {
				case "evicted":
					fallthrough
				case "expired":
					fallthrough
				case "del":
					fname := path.Join(config.CertDir, i+path.Ext(key))
					err := os.Remove(fname)
					if err != nil {
						slog.Error("Remove", "err", err)
					}
				case "set":
					didOne, err := handleCert(ctx, i)
					if err != nil {
						slog.Error("handleCert", "err", err)
						continue
					}
					if didOne {
						needExec = true
					}
				default:
					slog.Warn("unhandled message", "msg", msg)
				}
			}
		}
		if needExec {
			if len(config.Cmd) > 0 {
				slog.Info("exec", "cmd", config.Cmd)
				cmd := exec.Command("sh", "-c", config.Cmd)
				outerr, err := cmd.CombinedOutput()
				if err != nil {
					slog.Error("exec", "err", err, "outerr", string(outerr))
				}
			}
		}
	}
}

func handleCert(ctx context.Context, cert string) (bool, error) {
	didOne := false
	for _, suf := range []string{".key", ".crt"} {
		var value struct {
			Value    []byte
			Modified time.Time
		}
		fname := path.Join(config.CertDir, cert+suf)
		key := config.KeyPrefix + "/certificates/" + config.AcmeDirName + "/" + cert + "/" + cert + suf
		val, err := client.Get(ctx, key).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				continue
			}
			return false, err
		}
		val = strings.TrimPrefix(val, config.ValuePrefix)
		err = json.Unmarshal([]byte(val), &value)
		if err != nil {
			return false, err
		}
		finfo, err := os.Stat(fname)
		if err == nil && finfo.ModTime().UTC() == value.Modified.UTC() && finfo.Size() == int64(len(value.Value)) {
			continue
		} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return false, err
		}
		f, err := os.OpenFile(fname, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
		if err != nil {
			return false, err
		}
		n, err := f.Write(value.Value)
		if n != len(value.Value) {
			f.Close()
			return false, err
		}
		err = f.Close()
		if err != nil {
			return false, err
		}
		err = os.Chtimes(fname, value.Modified, value.Modified)
		if err != nil {
			return false, err
		}
		didOne = true
	}
	return didOne, nil
}
