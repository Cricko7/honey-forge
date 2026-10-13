package redistrap

import (
	"fmt"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestRandomSeedHasConsistentReferences(t *testing.T) {
	if _, err := exec.LookPath("redis-server"); err != nil {
		t.Skip("redis-server required; run in the Redis decoy image")
	}

	create := func() *redis.Client {
		t.Helper()
		p, err := startRedis(t.Context(), "bait pass\nwith config-like text")
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(p.stop)
		c := redis.NewClient(&redis.Options{Network: "unix", Addr: p.socket, Password: "bait pass\nwith config-like text"})
		t.Cleanup(func() { c.Close() })
		return c
	}

	a, b := create(), create()
	users, err := a.SMembers(t.Context(), "users:active").Result()
	if err != nil || len(users) < 12 || len(users) > 32 {
		t.Fatalf("users=%d err=%v", len(users), err)
	}

	var first, second []map[string]string
	for i := range 12 {
		id := fmt.Sprint(1001 + i)
		x, err := a.HGetAll(t.Context(), "user:"+id).Result()
		if err != nil || !strings.HasSuffix(x["email"], "@shop.example") {
			t.Fatalf("synthetic user=%v err=%v", x, err)
		}

		y, err := b.HGetAll(t.Context(), "user:"+id).Result()
		if err != nil {
			t.Fatal(err)
		}

		first, second = append(first, x), append(second, y)
	}

	if reflect.DeepEqual(first, second) {
		t.Fatal("independent Redis traps have identical synthetic data")
	}

	orders, err := a.LRange(t.Context(), "orders:recent", 0, -1).Result()
	if err != nil || len(orders) < len(users) {
		t.Fatalf("orders=%d err=%v", len(orders), err)
	}

	for _, id := range orders {
		order, err := a.HGetAll(t.Context(), "order:"+id).Result()
		if err != nil {
			t.Fatal(err)
		}

		if exists, err := a.Exists(t.Context(), "user:"+order["user_id"]).Result(); err != nil || exists != 1 {
			t.Fatalf("order references nonexistent user: %v %v", order, err)
		}

		items, err := a.HGetAll(t.Context(), "order:"+id+":items").Result()
		if err != nil || len(items) != 1 {
			t.Fatalf("order items=%v err=%v", items, err)
		}

		for product, quantity := range items {
			price, err := a.HGet(t.Context(), "product:"+product, "price_minor").Int()
			if err != nil {
				t.Fatal(err)
			}

			q, err := strconv.Atoi(quantity)
			if err != nil || order["total_minor"] != strconv.Itoa(q*price) {
				t.Fatalf("inconsistent order total: %v %v", order, err)
			}
		}
	}
}
