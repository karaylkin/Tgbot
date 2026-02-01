package main

import (
	"log"
	"net/http"

	"tor_project/internal/config"
	"tor_project/internal/db"
	"tor_project/internal/httpapi"
	"tor_project/internal/network"
	"tor_project/internal/service"
	"tor_project/internal/telegram"
)

func main() {
	// 1. Загрузка конфигурации
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Ошибка конфигурации: %v", err)
	}

	log.Println("=== TOR BOOK BOT STARTING ===")

	// 2. Инициализация сети (Tor)
	torClient, err := network.NewTorClient(cfg.TorProxyAddr)
	if err != nil {
		log.Fatalf("Fatal: Ошибка Tor: %v", err)
	}

	// 3. Инициализация Сервиса (Бизнес-логика)
	// Создаем сервис ДО бота, чтобы передать его внутрь
	svc := service.NewFlibustaClient(torClient, cfg.FlibustaURL)

	// 3.1 Инициализация БД
	store, err := db.Open(cfg.SQLitePath)
	if err != nil {
		log.Fatalf("Ошибка БД: %v", err)
	}
	defer store.Close()
	log.Printf("SQLite: %s", cfg.SQLitePath)
	log.Printf("Storage: %s", cfg.StorageDir)

	// 3.2 HTTP API для Mini App
	api := httpapi.New(store, cfg.StorageDir, cfg.TelegramToken)
	go func() {
		log.Printf("HTTP API запущен на %s", cfg.HTTPAddr)
		if err := http.ListenAndServe(cfg.HTTPAddr, api.Handler()); err != nil {
			log.Fatalf("Ошибка HTTP API: %v", err)
		}
	}()

	// 4. Инициализация Бота
	// ВАЖНО: Мы передаем svc (сервис) вторым аргументом!
	// (Убедись, что ты обновил файл internal/telegram/bot.go, как в инструкции выше)
	bot, err := telegram.NewBot(cfg.TelegramToken, svc, store, cfg.StorageDir, cfg.MiniAppURL)
	if err != nil {
		log.Fatalf("Ошибка при создании бота: %v", err)
	}

	// 5. Запуск Бота
	log.Println("Бот запущен! Открой Telegram и напиши /start или название книги.")

	// Эта функция блокирует выполнение (вечный цикл),
	// пока ты не остановишь программу (Ctrl+C).
	bot.Start()
}
