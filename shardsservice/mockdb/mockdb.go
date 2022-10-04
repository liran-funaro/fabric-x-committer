package mockdb

type mockdb struct {
}

func Open(path string) (*mockdb, error) {
	return &mockdb{}, nil
}

func (m *mockdb) DoNotExist(keys [][]byte) ([]bool, error) {
	l := len(keys)
	out := make([]bool, l)

	for i := 0; i < l; i++ {
		out[i] = true
	}

	return out, nil
}

func (m *mockdb) Commit(keys [][]byte) error {
	return nil
}

func (m *mockdb) Close() {

}
