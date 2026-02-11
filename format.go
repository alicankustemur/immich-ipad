package main

import (
	"fmt"
	"time"
)

type PhotoInfo struct {
	ID       string `json:"id"`
	Date     string `json:"date"`
	City     string `json:"city"`
	Index    int    `json:"index"`
	Total    int    `json:"total"`
	cityDone bool
}

var turkishMonths = []string{
	"Ocak", "Şubat", "Mart", "Nisan", "Mayıs", "Haziran",
	"Temmuz", "Ağustos", "Eylül", "Ekim", "Kasım", "Aralık",
}

func formatDate(isoDate string) string {
	t, err := time.Parse(time.RFC3339Nano, isoDate)
	if err != nil {
		t, err = time.Parse("2006-01-02T15:04:05.000Z", isoDate)
		if err != nil {
			return ""
		}
	}
	return fmt.Sprintf("%d %s %d", t.Day(), turkishMonths[t.Month()-1], t.Year())
}
