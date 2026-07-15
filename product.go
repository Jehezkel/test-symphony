package main

import (
	"errors"
	"sort"
	"sync"
)

var errProductNotFound = errors.New("product not found")

type product struct {
	ID    int
	Name  string
	Price string
	EAN   string
}

type productStore struct {
	mu     sync.RWMutex
	nextID int
	items  map[int]product
}

func newProductStore() *productStore {
	return &productStore{nextID: 1, items: make(map[int]product)}
}

func (s *productStore) list() []product {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]product, 0, len(s.items))
	for _, item := range s.items {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

func (s *productStore) create(name, price, ean string) product {
	s.mu.Lock()
	defer s.mu.Unlock()

	item := product{ID: s.nextID, Name: name, Price: price, EAN: ean}
	s.items[item.ID] = item
	s.nextID++
	return item
}

func (s *productStore) get(id int) (product, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.items[id]
	if !ok {
		return product{}, errProductNotFound
	}
	return item, nil
}

func (s *productStore) update(id int, name, price, ean string) (product, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.items[id]; !ok {
		return product{}, errProductNotFound
	}
	item := product{ID: id, Name: name, Price: price, EAN: ean}
	s.items[id] = item
	return item, nil
}

func (s *productStore) delete(id int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.items[id]; !ok {
		return errProductNotFound
	}
	delete(s.items, id)
	return nil
}
