package pkutil

type Option[T any] struct {
	Has bool
	Val T
}

func (o *Option[T]) Value() *T {
	if !o.Has {
		return nil
	}
	return &o.Val
}
