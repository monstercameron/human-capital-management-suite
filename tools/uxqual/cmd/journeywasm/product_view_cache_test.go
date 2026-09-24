package main

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestProductViewCacheRevisitAgeAndBound(t *testing.T) {
	now := time.Unix(1000, 0)
	c := newProductViewCache(2, time.Minute, func() time.Time { return now })
	docs := productui.View{Page: productui.PageDocs, DocumentID: "doc-1"}
	c.Put("/workspace/app/docs?document=doc-1", docs)
	if got, ok := c.Get("/workspace/app/docs?document=doc-1"); !ok || got.DocumentID != "doc-1" {
		t.Fatalf("revisit = (%v, %v), want the cached document view", got.DocumentID, ok)
	}
	c.Put("/workspace/app/chat", productui.View{Page: productui.PageChat})
	c.Put("/workspace/app/home", productui.View{Page: productui.PageHome})
	if _, ok := c.Get("/workspace/app/docs?document=doc-1"); ok {
		t.Fatal("the oldest entry must be evicted past the bound")
	}
	now = now.Add(2 * time.Minute)
	if _, ok := c.Get("/workspace/app/chat"); ok {
		t.Fatal("an entry older than the TTL must not paint")
	}
}

func TestProductViewCacheSkipsLiveAndFailedPages(t *testing.T) {
	c := newProductViewCache(4, time.Minute, time.Now)
	c.Put("/workspace/app/journeys", productui.View{Page: productui.PageJourneys})
	c.Put("/workspace/app/people", productui.View{Page: productui.PagePeople, LoadError: "boom"})
	if _, ok := c.Get("/workspace/app/journeys"); ok {
		t.Fatal("journeys render from the live store and must not be cached")
	}
	if _, ok := c.Get("/workspace/app/people"); ok {
		t.Fatal("a failed read must not be painted again")
	}
}
