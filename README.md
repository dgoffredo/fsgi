File System Gateway Interface
=============================
The File System Gateway Interface (FSGI) is an unpronounceable initialism.

It's akin to [CGI][1], but rather than using environment variables and the
standard input and output files to convey HTTP request and response
information, FSGI uses the file system instead.

Consider the request:
```
POST /foo/bar/baz?x=23&y=hello HTTP/1.1
Content-Type: text/plain
Content-Length: 6
x-something-special: la,la,la

hello!

```

An FSGI server would spawn a request handling process whose current directory
has the following structure:
```
request/
  method
  path
  query/
    x
    y
  headers/
    Content-Type
    Content-Length
    X-Something-Special
  body

response/
  headers/    
```

The FSGI server would then wait for the request handling process to terminate.
If the exit status of the request handling process is not zero, then the FSGI
server will return a 500 error to the client.  Otherwise, the FSGI server
expects the request handling process to have added zero or more files to the
`response` directory:

- `response/status` contains the HTTP response status, e.g. `404`.  If the
  status file is absent, then `200` is used instead.
- `response/headers/` contains a file for each response header.  The name of
  each file is the header name, and the content of each file is the header
  value.  "Content-Length" is ignored, and "Content-Type" may be omitted.
- `response/body` is a regular file containing the response body.  The
  response's "Content-Length" will be equal to the length of this file.  If
  `response/headers/Content-Type` is absent, then "Content-Type" will be
  guessed using the first 512 bytes of the body file.  If `response/body` is
  absent, then the response will have no body.

The names of headers are converted to [canonical format][2] for requests and
responses.  That is, a request handling process can assume that file names in
`request/headers` are in canonical format, and the FSGI server will deliver
response headers in canonical format regardless of the casing of file names in
`response/headers`.

[1]: https://en.wikipedia.org/wiki/Common_Gateway_Interface
[2]: https://pkg.go.dev/net/http#CanonicalHeaderKey
