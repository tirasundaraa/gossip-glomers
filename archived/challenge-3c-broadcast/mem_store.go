package main

import "sync"

type MemStore struct {
	mu sync.RWMutex
	m  map[int]struct{}
}

func NewMemStore() *MemStore {
	return &MemStore{m: make(map[int]struct{})}
}

func (ms *MemStore) Put(k int, v struct{}) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	ms.m[k] = v

	return nil
}

func (ms *MemStore) IsExist(k int) bool {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	_, exist := ms.m[k]

	return exist
}

func (ms *MemStore) Keys() []int {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	kk := make([]int, 0)
	for k, _ := range ms.m {
		kk = append(kk, k)
	}

	return kk
}
