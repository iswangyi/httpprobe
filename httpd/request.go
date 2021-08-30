package httpd

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"strconv"
	"strings"
)

type Request struct {
	Method      string
	URL         *url.URL
	Proto       string
	Header      Header
	Body        io.Reader
	RemoteAddr  string
	RequestURL  string
	conn        *conn
	cookies     map[string]string
	queryString map[string]string
}

func readRequest(c *conn) (*Request, error) {
	r := new(Request)
	r.conn = c
	r.RemoteAddr = c.rwc.RemoteAddr().String()
	line, err := readLine(c.bufr)
	if err != nil {
		log.Println("readLine:", err)
		return nil, errors.New("readLine error")
	}
	_, err = fmt.Sscanf(string(line), "%s%s%s", &r.Method, &r.RequestURL, &r.Proto)
	if err != nil {
		return nil, errors.New("Sccanf err")
	}
	r.URL, err = url.ParseRequestURI(r.RequestURL)
	if err != nil {
		return nil, errors.New("ParseURL error")
	}
	//解析queryString
	r.parseQuery()
	//读header
	r.Header, err = readHeader(c.bufr)
	if err != nil {
		return nil, errors.New("read header err ")
	}
	const noLimit = (1 << 63) - 1 // body的读取无需进行字节限制
	r.conn.lr.N = noLimit
	//设置body
	r.setupBody()
	return r, nil
}

/*ReadLine会借助到bufio.Reader的缓存切片，如果一行大小超过了缓存的大小，
这也会无法达到读出一行的要求，这时isPrefix会设置成true，代表只读取了一部分。
只要isPrefix一直为true，我们则一直读取，并将读取的部分汇总在一起，直至读到\r\n
*/
func readLine(bufr *bufio.Reader) ([]byte, error) {
	p, isPrefix, err := bufr.ReadLine()
	if err != nil {
		return p, err
	}
	var l []byte
	for isPrefix {
		l, isPrefix, err = bufr.ReadLine()
		if err != nil {
			break
		}
		p = append(p, l...)
	}
	return p, nil
}

func (r *Request) parseQuery() {
	//r.URL.RawQuery="name=gu&token=1234"
	r.queryString = parseQuery(r.URL.RawQuery)
}

func parseQuery(RawQuery string) map[string]string {
	parts := strings.Split(RawQuery, "&")
	queries := make(map[string]string, len(parts))
	for _, part := range parts {
		index := strings.IndexByte(part, '=')
		if index == -1 || index == len(part)-1 {
			continue
		}
		queries[strings.TrimSpace(part[:index])] = strings.TrimSpace(part[index+1:])
	}
	return queries
}

func readHeader(bufr *bufio.Reader) (Header, error) {
	header := make(Header)
	for {
		line, err := readLine(bufr)
		if err != nil {
			return nil, err
		}
		//如果读到/r/n/r/n,代表报文的首部结束
		if len(line) == 0 {
			break
		}
		//example: Connection:keep-alive
		i := bytes.IndexByte(line, ':')
		if i == -1 {
			return nil, errors.New("unsupported protocol")
		}
		if i == len(line)-1 {
			continue
		}
		k, v := string(line[:i]), strings.TrimSpace(string(line[i+1:]))
		header[k] = append(header[k], v)
	}
	return header, nil
}

//解析用户Body
type eofReader struct{}

func (er *eofReader) Read([]byte) (n int, err error) {
	return 0, io.EOF
}

func (r *Request) setupBody() {
	//POST和PUT以外的方法不允许设置报文主体
	if r.Method != "POST" && r.Method != "PUT" {
		r.Body = &eofReader{}
	} else if cl := r.Header.Get("Content-Length"); cl != "" {
		contentLength, err := strconv.ParseInt(cl, 10, 64)
		if err != nil {
			r.Body = &eofReader{}
			return
		}
		r.Body = io.LimitReader(r.conn.bufr, contentLength)

	} else {
		r.Body = &eofReader{}
	}
}

func (r *Request) Query(name string) string {
	return r.queryString[name]
}

func (r *Request) Cookie(name string) string {
	if r.cookies == nil { //将cookie的解析滞后到第一次cookie查询处
		r.parseCookies()
	}
	return r.cookies[name]
}

func (r *Request) parseCookies() {
	if r.cookies != nil {
		return
	}
	r.cookies = make(map[string]string)
	//判断是否请求是否有cookie
	rawCookies, ok := r.Header["Cookie"]
	if !ok {
		return
	}
	for _, line := range rawCookies {
		//example(line): uuid=12314753; tid=1BDB9E9; HOME=1(见上述的http请求报文)
		kvs := strings.Split(strings.TrimSpace(line), ";")
		if len(kvs) == 1 && kvs[0] == "" {
			continue
		}
		for i := 0; i < len(kvs); i++ {
			//example(kvs[i]): uuid=12314753
			index := strings.IndexByte(kvs[i], '=')
			if index == -1 {
				continue
			}
			r.cookies[strings.TrimSpace(kvs[i][:index])] = strings.TrimSpace(kvs[i][index+1:])
		}
	}
	return
}
