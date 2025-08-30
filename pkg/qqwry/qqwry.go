package qqwry

import (
	_ "embed"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strings"

	"github.com/lionsoul2014/ip2region/binding/golang/xdb"
)

//go:embed ip2region.xdb
var ipData []byte

func initXDBSearcher() (*xdb.Searcher, error) {
	// 将嵌入的 XDB 数据写入临时文件
	tmpfile, err := ioutil.TempFile("", "ip2region.xdb")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmpfile.Name()) // 清理临时文件

	if _, err := tmpfile.Write(ipData); err != nil {
		return nil, err
	}
	if err := tmpfile.Close(); err != nil {
		return nil, err
	}

	// 初始化 XDB 搜索器
	searcher, err := xdb.NewWithFileOnly(tmpfile.Name())
	if err != nil {
		return nil, err
	}

	return searcher, nil
}

func GetLocationByIP(ip string) (string, error) {
	ipAddr := net.ParseIP(ip)
	if ipAddr == nil {
		return "", fmt.Errorf("invalid IP address: %s", ip)
	}

	searcher, err := initXDBSearcher()
	if err != nil {
		return "", err
	}
	defer searcher.Close()

	region, err := searcher.SearchByStr(ip)
	if err != nil {
		return "", err
	}
	if len(region) > 0 {
		regions := strings.Split(region, "|")
		if len(regions) == 5 {
			if regions[4] == "内网IP" {
				region = "内网IP"
			} else {
				region = regions[2] + regions[3]
			}

		}
	}

	return region, nil
}
