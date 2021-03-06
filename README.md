# GWR: Get / Watch / Report -ing of operational data

GWR provides on demand access to operational data:
- define your data sources
- poll and/or watch them over HTTP or Redis Protocol

## Status: beta

GWR is currently in beta devolopment:
- basic support for get and watch are done, with a couple simple sources; these
  interfaces are not likely to change before 1.0
- reporting is not yet started, and is the major blocker before 1.0

# Using

GWR exposes a dual HTTP and RESP (Redis Protocol) interface.  Integrators may
specify the port, the example below uses 4040.

The following examples are against a running instance of `example_server/`.

## HTTP

Example for http:

```
$ curl localhost:4040/meta/nouns
- /meta/nouns formats: <no value>
- /request_log formats: <no value>
- /response_log formats: <no value>

$ curl -X WATCH localhost:4040/request_log&
$ curl -X WATCH localhost:4040/response_log&

$ curl localhost:8080/foo
404 page not found                                         # this is the normal curl output
GET /foo                                                   # this comes from the first watch-curl
404 19 text/plain; charset=utf-8                           # this comes from the first watch-curl
```

## Resp

```
$ redis-cli -p 4040 ls                                     # this is a convenience alias for "get /meta/nouns"
1) - /meta/nouns formats: <no value>
2) - /request_log formats: <no value>
3) - /response_log formats: <no value>

$ redis-cli -p 4040 monitor /request_log text /response_log text&
OK

$ curl localhost:8080/bar
404 page not found                                         # this is the curl output
/request_log> GET /bar                                     # this is from redis-cli
/response_log> 404 19 text/plain; charset=utf-8            # so is this, ordering not guaranteed
```

# Integration

To add gwr to a program, all you need to do is call:

```
gwrProto.ListenAndServe(":4040", nil)
```

This hosts dual protocol HTTP and RESP server on port 4040.

# Defining data sources

To define a data source, the easiest way is to implement the
`gwr.GenericDataSource` interface.

`TODO: example`

For now see `example_server/req_logger.go` and `example_server/res_logger.go`

# Running the example server

Should work by:
```
$ go run example_server/*.go
```

The example server hosts a dummy 404-ing web server on port `8080` and exposes
a request and response log GWR noun.  The HTTP and Resp usage examples above
are against it.
