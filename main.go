package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"first-max-bot/internal/api"
	"first-max-bot/internal/bot"
	"first-max-bot/internal/config"
	"first-max-bot/internal/storage"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	store, err := storage.Open(context.Background(), cfg.DataPath)
	if err != nil {
		log.Fatalf("Открытие базы: %v", err)
	}
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mux := http.NewServeMux()
	serverAPI := api.NewServer(cfg, store)

	maxBot := bot.NewBot(cfg, store)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		lastNotify := time.Time{}
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				store.ExpireInvitations()
				if time.Since(lastNotify) >= time.Minute {
					maxBot.NotifyClosures(ctx)
					lastNotify = time.Now()
				}
				saveCtx, stop := context.WithTimeout(ctx, 8*time.Second)
				if err := store.Save(saveCtx); err != nil {
					log.Printf("Сохранение базы: %v", err)
				}
				stop()
			}
		}
	}()
	serverAPI.SetBot(maxBot)
	serverAPI.RegisterRoutes(mux)

	addr := net.JoinHostPort(cfg.HTTPHost, cfg.Port)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Printf("==================================================================")
		log.Printf("⚔️  «Квиз-Дуэль» для MAX — команда «Алгоритм успеха»")
		log.Printf("🌐  Mini App: %s (HTTP listen %s)", cfg.BaseURL, addr)
		log.Printf("👥  Рейтинг и дуэли — из участников чата, куда добавлен бот")
		log.Printf("==================================================================")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	botDone := make(chan struct{})
	go func() {
		defer close(botDone)
		if err := maxBot.Run(ctx); err != nil {
			log.Printf("MAX Bot stopped with error: %v", err)
		}
	}()

	// Ожидание сигнала завершения
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)
	<-sigChan

	log.Println("🛑 Остановка сервисов «Квиз-Дуэль»...")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP shutdown: %v", err)
	}
	<-workerDone
	<-botDone
	finalCtx, finalCancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer finalCancel()
	if err := store.Save(finalCtx); err != nil {
		log.Printf("Последнее сохранение базы: %v", err)
	}

	fmt.Println("👋 До новых встреч на Арене!")
}
