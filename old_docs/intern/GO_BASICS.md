# 🐹 Go Basics — Just What You Need

You don't need to be a Go expert. You only need to understand the patterns used in this project.  
Read this once, then reference it as you explore the code.

---

## 1. How Go Programs Are Structured

Every Go file starts with a `package` name. A `package main` with a `func main()` is an executable program.

```go
package main

import "fmt"

func main() {
    fmt.Println("Hello, world")
}
```

This project has **4 separate programs** (`kv-store`, `kv-client`, `kv-chaos`, `kv-dashboard`). Each has its own `main()`.

---

## 2. Types and Structs

Go uses `struct` to group related data (like a class, but simpler):

```go
type Node struct {
    ID      string   // node's name, e.g. "node0"
    Alive   bool     // is it running?
    Index   int      // how many entries it has committed
}
```

Create and use one:
```go
n := Node{ID: "node0", Alive: true, Index: 42}
fmt.Println(n.ID)    // "node0"
```

---

## 3. Functions and Methods

```go
// Plain function
func add(a, b int) int {
    return a + b
}

// Method on a struct (the (n *Node) part means "attached to Node")
func (n *Node) IsLeader() bool {
    return n.State == "Leader"
}
```

You'll see this pattern constantly: `mgr.KillNode(id)`, `n.Bootstrap()`, etc.

---

## 4. Error Handling

Go functions return errors as values — there are no exceptions:

```go
result, err := someFunction()
if err != nil {
    log.Printf("something went wrong: %v", err)
    return err   // propagate up, OR handle here
}
// use result safely
```

You'll see `if err != nil` everywhere. That's normal — it's intentional.

---

## 5. Goroutines (Lightweight Threads)

```go
// Run something in the background
go myFunction()

// This doesn't wait for myFunction to finish
// The program continues immediately
```

Used everywhere for parallel operations — e.g. handling multiple HTTP requests at once.

---

## 6. Channels (Communication Between Goroutines)

```go
done := make(chan bool)

go func() {
    time.Sleep(1 * time.Second)
    done <- true   // send a value
}()

<-done   // wait until we receive
```

In this project you'll mainly see channels used for shutdown signals.

---

## 7. Maps

```go
m := make(map[string]string)
m["hello"] = "world"
val, ok := m["hello"]   // ok = true if key exists
```

The KV store's core data structure is literally `map[string]string`.

---

## 8. Interfaces

```go
type Animal interface {
    Sound() string
}

type Dog struct{}
func (d Dog) Sound() string { return "woof" }
// Dog automatically satisfies Animal — no "implements" keyword needed
```

HashiCorp Raft requires `server/fsm.go` to implement its `raft.FSM` interface (Apply, Snapshot, Restore methods).

---

## 9. Defer

```go
func doWork() {
    conn := openConnection()
    defer conn.Close()   // runs when function returns, no matter what
    // ... do work
}
```

You'll see `defer conn.Close()`, `defer cancel()`, `defer mu.Unlock()` everywhere — it's cleanup on exit.

---

## 10. Running Things from Code (`os/exec`)

The dashboard uses this to start `kv-store` as a child process:

```go
cmd := exec.Command("./kv-store", "-id=node0", "-raft=:12000")
cmd.Start()    // start, don't wait
cmd.Wait()     // wait for it to finish
cmd.Process.Kill()   // kill it
cmd.Process.Signal(syscall.SIGSTOP)  // freeze it
```

---

## 11. HTTP Handlers

```go
http.HandleFunc("/api/kill/", func(w http.ResponseWriter, r *http.Request) {
    nodeID := r.URL.Path[len("/api/kill/"):]   // extract from URL
    // ... do something
    json.NewEncoder(w).Encode(map[string]string{"status": "killed"})
})
http.ListenAndServe(":8080", nil)
```

This is exactly how `/api/pause` and `/api/resume` are implemented in `cmd/dashboard/main.go`.

---

## 12. Build & Run

```bash
# Build
GOCACHE=/tmp/go-cache GOTMPDIR=/tmp/gobuild go build -o kv-dashboard ./cmd/dashboard/

# Run
./kv-dashboard -nodes=3 -port=8080

# Flags are parsed with the 'flag' package
var port = flag.Int("port", 8080, "port to listen on")
flag.Parse()
```

That's all the Go you need. Open `cmd/dashboard/main.go` and it should now make sense.
