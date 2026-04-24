package testkit

type FakeStore struct {
	records map[string]string
}

func NewFakeStore() *FakeStore {
	return &FakeStore{records: make(map[string]string)}
}

func (f *FakeStore) Put(key, value string) {
	f.records[key] = value
}

func (f *FakeStore) Get(key string) (string, bool) {
	value, ok := f.records[key]
	return value, ok
}
