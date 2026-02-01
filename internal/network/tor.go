package network

import (
	"fmt"
	"net/http"
	"time"

	"golang.org/x/net/proxy"
)

// NewTorClient создает http.Client, работающий через SOCKS5.
func NewTorClient(proxyAddr string) (*http.Client, error) {
	// Создаем дилер (дозвонщик)
	dialer, err := proxy.SOCKS5("tcp", proxyAddr, nil, proxy.Direct)
	if err != nil {
		return nil, fmt.Errorf("ошибка подключения к SOCKS5 (%s): %w", proxyAddr, err)
	}

	// Настраиваем транспорт
	transport := &http.Transport{
		Dial:              dialer.Dial,
		DisableKeepAlives: true, // Для CLI утилит лучше отключать
	}

	// Возвращаем готовый клиент
	return &http.Client{
		Transport: transport,
		Timeout:   time.Minute * 2, // Tor медленный, даем 2 минуты
	}, nil
}
