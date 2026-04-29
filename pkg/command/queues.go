package command

import (
	"Rshell/pkg/connection"
	"Rshell/pkg/utils"
	"strings"
	"sync"
)

// --- Command Queue ---

type ClientCommandQueue struct {
	mu     sync.Mutex
	queues map[string][][]byte
}

var CommandQueues = &ClientCommandQueue{
	queues: make(map[string][][]byte),
}

func (c *ClientCommandQueue) AddCommand(clientID string, command []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.queues[clientID]; !exists {
		c.queues[clientID] = [][]byte{}
	}
	c.queues[clientID] = append(c.queues[clientID], command)
}

func (c *ClientCommandQueue) GetCommand(clientID string) (command []byte, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	queue, exists := c.queues[clientID]
	if !exists {
		c.queues[clientID] = [][]byte{}
		return []byte{}, false
	}
	if len(queue) == 0 {
		return []byte{}, false
	}
	command, c.queues[clientID] = queue[0], queue[1:]
	return command, true
}

// --- Pid Queue ---

type PidQueue struct {
	mutex  sync.Mutex
	Queues map[string]chan string
}

var VarPidQueue = &PidQueue{Queues: make(map[string]chan string)}

func (q *PidQueue) Add(uid string, pids string) {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	if _, exists := q.Queues[uid]; !exists {
		q.Queues[uid] = make(chan string, 1)
	}
	select {
	case <-q.Queues[uid]:
	default:
	}
	q.Queues[uid] <- pids
}

func (q *PidQueue) GetOrCreateQueue(uid string) chan string {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	if _, exists := q.Queues[uid]; !exists {
		q.Queues[uid] = make(chan string, 1)
	}
	return q.Queues[uid]
}

// --- Drives Queue ---

type DrivesQueue struct {
	mutex  sync.Mutex
	Queues map[string]chan []string
}

var VarDrivesQueue = &DrivesQueue{Queues: make(map[string]chan []string)}

func (q *DrivesQueue) Add(uid string, files []string) {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	if _, exists := q.Queues[uid]; !exists {
		q.Queues[uid] = make(chan []string, 1)
	}
	select {
	case <-q.Queues[uid]:
	default:
	}
	q.Queues[uid] <- files
}

func (q *DrivesQueue) GetOrCreateQueue(uid string) chan []string {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	if _, exists := q.Queues[uid]; !exists {
		q.Queues[uid] = make(chan []string, 1)
	}
	return q.Queues[uid]
}

// --- File Content Queue ---

type FileContentQueue struct {
	mutex  sync.Mutex
	Queues map[string]map[string]chan string
}

var VarFileContentQueue = &FileContentQueue{Queues: make(map[string]map[string]chan string)}

func (q *FileContentQueue) Add(uid string, filePath, files string) {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	if q.Queues[uid] == nil {
		q.Queues[uid] = make(map[string]chan string)
	}
	if _, exists := q.Queues[uid][filePath]; !exists {
		q.Queues[uid][filePath] = make(chan string, 1)
	}
	select {
	case <-q.Queues[uid][filePath]:
	default:
	}
	q.Queues[uid][filePath] <- files
}

func (q *FileContentQueue) GetOrCreateQueue(uid string, filePath string) chan string {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	if q.Queues[uid] == nil {
		q.Queues[uid] = make(map[string]chan string)
	}
	if _, exists := q.Queues[uid][filePath]; !exists {
		q.Queues[uid][filePath] = make(chan string, 1)
	}
	return q.Queues[uid][filePath]
}

// --- File Browser Queue ---

type FileBrowserQueue struct {
	mutex  sync.Mutex
	Queues map[string]chan string
}

var VarFileBrowserQueue = &FileBrowserQueue{Queues: make(map[string]chan string)}

func (q *FileBrowserQueue) Add(uid string, files string) {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	if _, exists := q.Queues[uid]; !exists {
		q.Queues[uid] = make(chan string, 1)
	}
	select {
	case <-q.Queues[uid]:
	default:
	}
	q.Queues[uid] <- files
}

func (q *FileBrowserQueue) GetOrCreateQueue(uid string) chan string {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	if _, exists := q.Queues[uid]; !exists {
		q.Queues[uid] = make(chan string, 1)
	}
	return q.Queues[uid]
}

type FileNode struct {
	Name         string      `json:"name"`
	Size         string      `json:"size"`
	Type         string      `json:"type"`
	Path         string      `json:"path"`
	ModifiedTime string      `json:"modifiedTime,omitempty"`
	Children     []*FileNode `json:"children,omitempty"`
}

var UidFileBrowser = make(map[string][]*FileNode)
var fileBrowserMutex sync.Mutex

func ParseDirectoryString(uid string, data string) []*FileNode {
	fileBrowserMutex.Lock()
	defer fileBrowserMutex.Unlock()

	lines := strings.Split(data, "\n")
	if len(lines) < 4 {
		return UidFileBrowser[uid]
	}

	currentDir := strings.TrimSuffix(lines[0], "/*")
	currentDir = strings.Replace(currentDir, "\\", "/", -1)
	currentDir = strings.TrimSuffix(currentDir, "/")

	isWindows := len(currentDir) >= 2 && currentDir[1] == ':'

	var rootName string
	if isWindows {
		rootName = currentDir[:2]
	} else {
		rootName = "/"
	}

	if _, exists := UidFileBrowser[uid]; !exists {
		UidFileBrowser[uid] = []*FileNode{{
			Name:     rootName,
			Type:     "D",
			Path:     rootName,
			Children: []*FileNode{},
		}}
	}

	var children []*FileNode
	for _, line := range lines[3:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 4 {
			continue
		}

		child := &FileNode{
			Name: parts[3],
			Type: parts[0],
			Path: currentDir + "/" + parts[3],
		}

		if parts[0] == "F" {
			child.Size = utils.BytesToSize(parts[1])
			child.ModifiedTime = parts[2]
		} else {
			child.ModifiedTime = parts[2]
			child.Children = []*FileNode{}
		}

		children = append(children, child)
	}

	updateFileTree(uid, currentDir, children)
	return UidFileBrowser[uid]
}

func updateFileTree(uid, currentDir string, children []*FileNode) {
	var parts []string
	if currentDir == "/" {
		parts = []string{}
	} else if strings.HasPrefix(currentDir, "/") {
		if currentDir != "/" {
			parts = strings.Split(currentDir[1:], "/")
		}
	} else if len(currentDir) >= 2 && currentDir[1] == ':' {
		if len(currentDir) > 2 {
			pathPart := currentDir[2:]
			if strings.HasPrefix(pathPart, "/") {
				pathPart = pathPart[1:]
			}
			if pathPart != "" {
				parts = strings.Split(pathPart, "/")
			}
		}
	}

	root := getRootNode(uid, currentDir)
	if root == nil {
		return
	}

	targetDir := root
	for _, part := range parts {
		found := false
		for _, child := range targetDir.Children {
			if child.Type == "D" && child.Name == part {
				targetDir = child
				found = true
				break
			}
		}
		if !found {
			var newPath string
			if targetDir.Path == "/" {
				newPath = "/" + part
			} else if strings.HasSuffix(targetDir.Path, "/") {
				newPath = targetDir.Path + part
			} else {
				newPath = targetDir.Path + "/" + part
			}
			newDir := &FileNode{
				Name:     part,
				Type:     "D",
				Path:     newPath,
				Children: []*FileNode{},
			}
			targetDir.Children = append(targetDir.Children, newDir)
			targetDir = newDir
		}
	}

	existingMap := make(map[string]*FileNode)
	for _, child := range targetDir.Children {
		key := child.Name + ":" + child.Type
		existingMap[key] = child
	}

	var newChildren []*FileNode
	for _, newChild := range children {
		key := newChild.Name + ":" + newChild.Type
		if existingChild, exists := existingMap[key]; exists {
			existingChild.Size = newChild.Size
			existingChild.ModifiedTime = newChild.ModifiedTime
			if newChild.Type == "D" {
				newChild.Children = existingChild.Children
			}
			newChildren = append(newChildren, existingChild)
			delete(existingMap, key)
		} else {
			newChildren = append(newChildren, newChild)
		}
	}

	for _, remaining := range existingMap {
		if remaining.Type == "D" {
			newChildren = append(newChildren, remaining)
		}
	}

	targetDir.Children = newChildren
}

func getRootNode(uid, path string) *FileNode {
	if _, exists := UidFileBrowser[uid]; !exists {
		return nil
	}

	var rootName string
	if strings.HasPrefix(path, "/") {
		rootName = "/"
	} else if len(path) >= 2 && path[1] == ':' {
		rootName = path[:2]
	} else {
		return nil
	}

	for _, node := range UidFileBrowser[uid] {
		if node.Name == rootName {
			return node
		}
	}
	return nil
}

func ParseDrives(uid string, drives []string) []*FileNode {
	for _, drive := range drives {
		if !exsitPan(UidFileBrowser[uid], drive) {
			UidFileBrowser[uid] = append(UidFileBrowser[uid], &FileNode{Name: drive, Type: "D", Path: drive})
		}
	}
	return UidFileBrowser[uid]
}

func exsitPan(filenode []*FileNode, pan string) bool {
	for _, file := range filenode {
		if file.Name == pan {
			return true
		}
	}
	return false
}

func isInChild(root *FileNode, child *FileNode) bool {
	for _, childNode := range root.Children {
		if childNode.Name == child.Name && childNode.Type == child.Type {
			return true
		}
	}
	return false
}

func deleteChild(root []*FileNode, child *FileNode) []*FileNode {
	var result []*FileNode
	for _, childNode := range root {
		if childNode.Name != child.Name || childNode.Type != child.Type {
			result = append(result, childNode)
		}
	}
	return result
}

// --- SOCKS5 Queue ---

type Socks5Queue struct {
	mutex  sync.Mutex
	Queues map[string]map[string]chan string
}

var VarSocks5Queue = &Socks5Queue{Queues: make(map[string]map[string]chan string)}

func (q *Socks5Queue) Add(uid string, dataMd5, rawData string) {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	if realUID, exists := connection.GlobalUIDMapper.GetRealUID(uid); exists {
		uid = realUID
	}

	if q.Queues[uid] == nil {
		q.Queues[uid] = make(map[string]chan string)
	}
	if _, exists := q.Queues[uid][dataMd5]; !exists {
		q.Queues[uid][dataMd5] = make(chan string, 1)
	}
	select {
	case <-q.Queues[uid][dataMd5]:
	default:
	}
	q.Queues[uid][dataMd5] <- rawData
}

func (q *Socks5Queue) GetOrCreateQueue(uid string, dataMd5 string) chan string {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	if realUID, exists := connection.GlobalUIDMapper.GetRealUID(uid); exists {
		uid = realUID
	}

	if q.Queues[uid] == nil {
		q.Queues[uid] = make(map[string]chan string)
	}
	if _, exists := q.Queues[uid][dataMd5]; !exists {
		q.Queues[uid][dataMd5] = make(chan string, 1)
	}
	return q.Queues[uid][dataMd5]
}
