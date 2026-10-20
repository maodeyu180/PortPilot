package main

import (
	"context"
	"flag"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"portpilot/internal/control"
	"portpilot/web"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "管理面板监听地址")
	data := flag.String("data", "./data", "私有数据目录")
	initialize := flag.Bool("init", false, "初始化数据和空内核配置后退出，保留已有数据")
	healthcheck := flag.Bool("healthcheck", false, "检查指定监听地址的面板是否就绪后退出")
	flag.Parse()
	if *initialize {
		if err := control.Bootstrap(*data, control.BootstrapOptions{
			PublicHost: os.Getenv("PORTPILOT_PUBLIC_HOST"), ControllerURL: os.Getenv("PORTPILOT_CONTROLLER_URL"),
			Username: os.Getenv("PORTPILOT_PROXY_USERNAME"), Password: os.Getenv("PORTPILOT_PROXY_PASSWORD"),
		}); err != nil {
			log.Fatal(err)
		}
		log.Print("初始化完成；已有数据保持原样，代理账号密码可在面板的访问认证中查看")
		return
	}
	if *healthcheck {
		if err := checkHealth(*listen); err != nil {
			log.Fatal(err)
		}
		return
	}
	password := os.Getenv("PORTPILOT_ADMIN_PASSWORD")
	_, _, err := net.SplitHostPort(*listen)
	if err != nil {
		log.Fatal("无效的监听地址")
	}
	store, err := control.OpenStore(*data)
	if err != nil {
		log.Fatal(err)
	}
	app, err := control.NewApp(store, password)
	if err != nil {
		log.Fatal(err)
	}
	assets, err := fs.Sub(web.Assets, "dist")
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go app.RunSync(ctx)
	server := &http.Server{Addr: *listen, Handler: app.Handler(assets), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	go func() {
		<-ctx.Done()
		stop, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		_ = server.Shutdown(stop)
	}()
	display := *listen
	if strings.HasPrefix(display, ":") {
		display = "127.0.0.1" + display
	}
	log.Printf("PortPilot v0.1.3 · http://%s", display)
	if app.SetupRequired() {
		log.Print("首次启动：请访问面板设置管理密码")
	}
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
