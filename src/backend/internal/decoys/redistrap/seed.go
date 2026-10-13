package redistrap

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"honey-forge/internal/contract"
)

// Every record is synthetic; .example addresses cannot belong to real customers.
func seedRedis(ctx context.Context, client *redis.Client) error {
	p := client.TxPipeline()
	p.Set(ctx, "app:mode", "production", 0)
	p.Set(ctx, "app:version", fmt.Sprintf("2.%d.%d", 10+rand.IntN(15), rand.IntN(10)), 0)
	p.HSet(ctx, "config:checkout", "currency", "RUB", "payment_provider", "sandbox", "reservation_ttl_seconds", "900")

	now := time.Now().UTC()
	firstNames := []string{"Ivan", "Anna", "Dmitry", "Elena", "Alexey", "Maria", "Pavel", "Olga"}
	lastNames := []string{"Petrov", "Smirnov", "Volkov", "Sokolov", "Morozov", "Orlov", "Lebedev", "Kuznetsov"}
	users := 12 + rand.IntN(21)

	for i := range users {
		id := fmt.Sprint(1001 + i)
		first, last := firstNames[rand.IntN(len(firstNames))], lastNames[rand.IntN(len(lastNames))]
		if first == "Anna" || first == "Elena" || first == "Maria" || first == "Olga" {
			last += "a"
		}

		p.HSet(ctx, "user:"+id, "id", id, "name", first+" "+last, "email", fmt.Sprintf("%s.%s.%s@shop.example", strings.ToLower(first), strings.ToLower(last), id), "tier", []string{"standard", "silver", "gold"}[rand.IntN(3)], "created_at", now.AddDate(0, 0, -30-rand.IntN(700)).Format(time.RFC3339))
		p.SAdd(ctx, "users:active", id)

		sessionKey := "session:web:" + string(contract.NewID())
		p.HSet(ctx, sessionKey, "user_id", id, "locale", "ru-RU", "last_seen_at", now.Add(-time.Duration(rand.IntN(120))*time.Minute).Format(time.RFC3339))
		p.Expire(ctx, sessionKey, time.Duration(4+rand.IntN(21))*time.Hour)
	}

	products := []struct {
		name         string
		price, stock int
	}{
		{"Wireless keyboard", 3490, 42}, {"USB-C hub", 2790, 76}, {"Laptop stand", 1990, 28}, {"Wireless mouse", 1590, 105},
	}

	for i, product := range products {
		id := fmt.Sprint(1001 + i)
		product.price += rand.IntN(16) * 100
		product.stock = 10 + rand.IntN(150)
		products[i] = product

		p.HSet(ctx, "product:"+id, "id", id, "sku", "ACC-"+id, "name", product.name, "price_minor", product.price*100, "currency", "RUB", "stock", product.stock)
		p.ZAdd(ctx, "catalog:popular", redis.Z{Score: float64(100 + rand.IntN(2000)), Member: id})
	}

	orderBase := now.Format("20060102")

	for i := range users + rand.IntN(20) {
		id := fmt.Sprintf("%s%04d", orderBase, i+1)
		product := rand.IntN(len(products))
		productID, quantity := fmt.Sprint(1001+product), 1+rand.IntN(3)
		status := []string{"paid", "shipped", "processing", "delivered"}[rand.IntN(4)]
		p.HSet(ctx, "order:"+id, "id", id, "user_id", fmt.Sprint(1001+rand.IntN(users)), "status", status, "total_minor", products[product].price*100*quantity, "currency", "RUB", "created_at", now.Add(-time.Duration(1+rand.IntN(720))*time.Hour).Format(time.RFC3339))
		p.HSet(ctx, "order:"+id+":items", productID, quantity)
		p.LPush(ctx, "orders:recent", id)
		p.XAdd(ctx, &redis.XAddArgs{Stream: "events:orders", Values: map[string]any{"order_id": id, "status": status}})
	}

	_, err := p.Exec(ctx)
	if err != nil {
		return fmt.Errorf("write synthetic dataset: %w", err)
	}

	return nil
}
