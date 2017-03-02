loggly
======

api client for loggly.  Provides an io.Writer interface for loggly that uploads via the bulk api

``` golang
	token := os.Getenv("LOGGLY_TOKEN")
	client := loggly.New(token, loggly.Interval(5*time.Second))
	client.Write([]byte("{\"hello\":\"world\"}\n"))
```

