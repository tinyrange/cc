package main

import (
	"encoding/json"
	"errors"
	"flag"
	"net"
	"net/http"
	"os"
	"time"

	"j5.nz/cc/client"
)

func main() {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	addr := fs.String("addr", "localhost:0", "Address to listen on")

	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(1)
	}

	l, err := net.Listen("tcp", *addr)
	if err != nil {
		panic(err)
	}

	if err := json.NewEncoder(os.Stdout).Encode(client.ServerHello{
		Addr: l.Addr().String(),
	}); err != nil {
		panic(err)
	}

	var svr http.Server

	mux := http.NewServeMux()

	// The <method> <path> syntax is a recent addition to Golang.

	// GET /healthz is used by the client to check if the server is healthy.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "ok", http.StatusOK)
	})

	// POST /shutdown is used by the client to request the server to shut down.
	mux.HandleFunc("POST /shutdown", func(w http.ResponseWriter, r *http.Request) {
		go func() {
			// sleep for a short time to allow the response to be sent before shutting down the server.
			time.Sleep(10 * time.Millisecond)

			if err := svr.Shutdown(r.Context()); err != nil {
				panic(err)
			}
		}()

		http.Error(w, "ok", http.StatusOK)
	})

	// GET /kernel is used by the client to get the current downloaded kernel status.
	// The response is a JSON object with the following format:
	// {
	//    "status": "downloaded" | "downloading" | "error",
	//    "error": "error message if status is error",
	//    "version": "kernel version if status is downloaded",
	//    "source": "alpine:<version>",
	// }
	mux.HandleFunc("GET /kernel", func(w http.ResponseWriter, r *http.Request) {
		// not implemented yet
		http.Error(w, "not implemented", http.StatusNotImplemented)
	})

	// POST /kernel/download is used by the client to request the server to download a kernel.
	mux.HandleFunc("POST /kernel/download", func(w http.ResponseWriter, r *http.Request) {
		// not implemented yet
		http.Error(w, "not implemented", http.StatusNotImplemented)
	})

	// GET /image is used by the client to get the list of downloaded images.
	// Each image is represented as a JSON object with the following format:
	// {
	//   "name": "image name",
	//   "source": "OCI image reference",
	//   "status": "downloaded" | "downloading" | "error",
	//   "error": "error message if status is error"
	// }
	mux.HandleFunc("GET /image", func(w http.ResponseWriter, r *http.Request) {
		// not implemented yet
		http.Error(w, "not implemented", http.StatusNotImplemented)
	})

	// GET /image/<name> is used by the client to get the status of a downloaded image.
	// <name> is a alias the client understands rather than a OCI image reference. For example, "ubuntu" could be an alias for "docker.io/library/ubuntu:latest".
	mux.HandleFunc("GET /image/{image}", func(w http.ResponseWriter, r *http.Request) {
		// not implemented yet
		http.Error(w, "not implemented", http.StatusNotImplemented)
	})

	// POST /image/<name> is used by the client to request the server to download an image.
	// A JSON body is expected with the following format:
	// {
	//   "source": "docker.io/library/ubuntu:latest"
	// }
	// The server streams the download progress back to the client as JSON objects with the following format:
	// {
	//   "status": "downloading" | "extracting" | "done" | "error",
	//   "blob": "", // the blob currently being downloaded or extracted, if applicable
	//   "progress": 0.0, // percentage of the download progress, between 0 and 100
	//   "error": "error message if status is error"
	// }
	mux.HandleFunc("POST /image/{image}", func(w http.ResponseWriter, r *http.Request) {
		// not implemented yet
		http.Error(w, "not implemented", http.StatusNotImplemented)
	})

	// GET /vm/supported is used by the client to check if the server supports running VMs.
	mux.HandleFunc("GET /vm/supported", func(w http.ResponseWriter, r *http.Request) {
		// not implemented yet
		http.Error(w, "not implemented", http.StatusNotImplemented)
	})

	// POST /vm is used by the client to request the server to start a VM with a specified image.
	// A JSON body is expected with the following format:
	// {
	//   "image": "image name"
	// }
	// The server streams the boot progress back to the client as JSON objects with the following format:
	// {
	//   "status": "downloading_kernel" | "extracting_kernel" | "booting" | "running" | "error",
	//   "error": "error message if status is error"
	// }
	// If the kernel is not downloaded, the server should download the kernel first and stream the download progress back to the client as JSON objects with the following format:
	// {
	//   "status": "downloading_kernel" | "extracting_kernel",
	//   "progress": 0.0, // percentage of the kernel download progress, between 0 and 100
	//   "error": "error message if status is error"
	// }
	mux.HandleFunc("POST /vm", func(w http.ResponseWriter, r *http.Request) {
		// not implemented yet
		http.Error(w, "not implemented", http.StatusNotImplemented)
	})

	// POST /vm/shutdown is used by the client to request the server to shut down the running VM.
	mux.HandleFunc("POST /vm/shutdown", func(w http.ResponseWriter, r *http.Request) {
		// not implemented yet
		http.Error(w, "not implemented", http.StatusNotImplemented)
	})

	// /vm/run is used by the client to request the server to run a command in the running VM.
	// The client is expected to send the request using WebSocket, and the server should upgrade the connection to WebSocket.
	// A JSON message is expected with the following format:
	// {
	//   "command": ["args"],
	// }
	// The server should stream the command output back to the client as JSON messages with the following format:
	// {
	//   "kind": "stdout" | "stderr" | "exit",
	//   "output": "command output",
	//   "error": "error message if command execution fails"
	// }
	// The client can send a JSON message with the following format to make requests to the server:
	// {
	//   "kind": "signal" | "stdin" | "close_stdin",
	//   "signal": "signal name", // e.g. "SIGINT"
	//   "input": "input to be sent to the command's stdin"
	// }
	mux.HandleFunc("/vm/run", func(w http.ResponseWriter, r *http.Request) {
		// not implemented yet
		http.Error(w, "not implemented", http.StatusNotImplemented)
	})

	svr = http.Server{
		Handler: mux,
	}

	if err := svr.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
		panic(err)
	}
}
