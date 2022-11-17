# rawhttp

rawhttp is a Go package for making HTTP requests in a raw way.


- Forked and adapted from [https://github.com/gorilla/http](https://github.com/gorilla/http) and [https://github.com/valyala/fasthttp](https://github.com/valyala/fasthttp)
- The original idea is inspired by [@tomnomnom/rawhttp](https://github.com/tomnomnom/rawhttp) work

# Example

First you need to declare a `server`

```go
...
...

func headers(w http.ResponseWriter, req *http.Request) {
	for name, headers := range req.Header {
		for _, h := range headers {
			fmt.Fprintf(w, "%v: %v\n", name, h)
		}
	}
}

func main() {
	http.HandleFunc("/headers", headers)
	if err := http.ListenAndServe(":10000", nil); err != nil {
		gologger.Fatal().Msgf("Could not listen and serve: %s\n", err)
	}
}
```

```
go run server.go
```

Second you need to start the client

```go
func main() {
    host := "127.0.0.1:10000"
	swg := sizedwaitgroup.New(25)
	pipeOptions := rawhttp.DefaultPipelineOptions
	pipeOptions.Host = host
	pipeOptions.MaxConnections = 1
	pipeclient := rawhttp.NewPipelineClient(pipeOptions)
	for i := 0; i < 50; i++ {
		swg.Add()
		go func(swg *sizedwaitgroup.SizedWaitGroup) {
			defer swg.Done()
			req, err := http.NewRequest("GET", host + "/headers", nil)
			if err != nil {
				log.Printf("Error sending request to API endpoint. %+v", err)
				return
			}
			req.Host = host
			req.Header.Set("Host", host)
			resp, err := pipeclient.Do(req)
			if err != nil {
				log.Printf("Error sending request to API endpoint. %+v", err)
				return
			}
			log.Printf("%+v\n", resp)
			_ = resp
		}(&swg)
	}

	swg.Wait()

}
```

```
go run client.go
```


# License

rawhttp is distributed under MIT License