package testdb

// Ptr returns a pointer to a copy of v, for optional fields in test values.
func Ptr[T any](v T) *T { return &v }
