package logger

func Error(args ...interface{}) {
	if len(args) == 1 {
		if args[0] == nil {
			return
		}
		if err, ok := args[0].(error); ok {
			Default.(*entry).logErr(err)
			return
		}
	}
	Default.(*entry).logLevel(2, ErrorLevel, args...)
}

func Info(v ...interface{}) {
	Default.(*entry).logLevel(1, InfoLevel, v...)
}
