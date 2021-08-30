package httpd

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
)

type conn struct {
	svr *Server
	rwc net.Conn
	//使用bufio写入缓存，减少io次数
	bufw *bufio.Writer
	bufr *bufio.Reader
	//限制读取大小
	lr *io.LimitedReader
}

func newConn(rwc net.Conn, svr *Server) *conn {
	lr := &io.LimitedReader{
		R: rwc,
		N: 1 << 20,
	}
	return &conn{
		svr:  svr,
		rwc:  rwc,
		bufw: bufio.NewWriterSize(rwc, 4<<10),
		bufr: bufio.NewReaderSize(lr, 4<<10),
		lr:   lr,
	}
}

func (c *conn) serve() {

	defer func() {
		if err := recover(); err != nil {
			log.Printf("panic recoverred,err:%v\n", err)
		}
		c.close()
	}()

	//http1.1支持keep-alive长链接，所以一个链接可能读出多个请求
	for {
		//读取请求
		req, err := readRequest(c)
		if err != nil {
			handleErr(err, c)
			return
		}
		fmt.Println(req)

		//写回复内容
		res := SetupResponse(c)
		c.svr.Handler.ServeHTTP(res, req)
		if err = c.bufw.Flush(); err != nil {
			return
		}
	}

}

func (c *conn) close()             { c.rwc.Close() }
func handleErr(err error, c *conn) { fmt.Println("ERR:::", err) }
