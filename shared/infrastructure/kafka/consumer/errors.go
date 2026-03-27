package consumer

type RetryExhaustedError struct {
	Err        error
	RetryCount int
}

func (e RetryExhaustedError) Error() string {
	return e.Err.Error()
}

func (e RetryExhaustedError) Unwrap() error {
	return e.Err
}
