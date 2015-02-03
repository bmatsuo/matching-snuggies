/*
Command snuggied is an HTTP server that exposes a REST API for 'slicing' 3D
models, converting them into G-code machine instructions for 3D printers.

	snuggied -http=:8888

Clients (host software or the snuggier tool) POST 3D mesh files to snuggied
and, after slicing is complete, snuggied exposes the resulting G-code for the
client to retreive as a GET.  Clients periodically poll the server during
slicing for status updates.  Clients may cancel an in-progress slicing job at
any point by issuing a DELETE request.

Call snuggied with the -h flag to see available command line configuration.

	snuggied -h

API Documentation

An HTTP API is exposed by snuggied for clients (snuggier) to use.

Create a job

The job begins with the client supplying a 3D mesh file for the server to
slice.

	POST /slicer/jobs
	Content-Type: muiltpart/form-data

		meshfile  3D mesh file (stl or amf)
		slicer    backend slicer program (only "slic3r" supported currently)
		preset    name of a preset backend configuration

	201 Created
	Content-Type: application/json

		slicerjob.Job

List jobs

The client may use this if interested in the status of multiple jobs.

	GET /slicer/jobs

	200 OK
	Content-Type: application/json

		slicerjob.Page of []slicerjob.Job

Get a job's status

Until the backend slicer has completed the client polls the job's status for
progress.

	GET /slicer/jobs/{id}

	200 OK
	Content-Type: application/json

		slicerjob.Job

Cancel a job

Cancelling a job removes it from internal queues and terminates the backend
slicing procedure if it has already begun.

	DELETE /slicer/jobs/{id}

	200 OK

Retrieve final g-code

After a job completes it's g-code output is available using the job id as a
key.

	GET /slicer/gcodes/{id}

	200 OK
	Content-Type: application/octet-stream

Retrieve an original mesh file

The mesh file originally given to a job. not in the critical path of printing.

	GET /slicer/meshes/{id}

	200 OK
	Content-Type: application/octet-stream

The contents of the original 3D mesh file are returned.  The content-type may
be more specific when the file has a known media type.

List backend presets

	GET /slicer/presets/{slicer}

	200 OK
	Content-Type: application/json

		slicerjob.SlicerPresets

*/
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"flag"

	"github.com/bmatsuo/matching-snuggies/slicerjob"
	"github.com/facebookgo/flagenv"
)

type SnuggieServer struct {
	Config map[string]string

	// Prefix should not end in a slash '/'.
	BaseURL       string
	Prefix        string
	Slic3r        string
	Slic3rPresets map[string]string
	DataDir       string

	LocalConsumer bool
	S             Scheduler
	C             Consumer
}

func (srv *SnuggieServer) RegisterHandlers(mux *http.ServeMux) http.Handler {
	mux.HandleFunc(srv.route("/jobs"), func(w http.ResponseWriter, r *http.Request) {
		// the request does not have an ID suffix on the url path so we are
		// either creating or listing jobs.
		switch r.Method {
		case "POST":
			srv.CreateJob(w, r)
		case "GET":
			srv.ListJobs(w, r)
		default:
			http.Error(w, "only GET, POST are allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc(srv.route("/jobs/"), func(w http.ResponseWriter, r *http.Request) {
		// the request has an ID suffix on the url path so we are showing a
		// single job resource.
		switch r.Method {
		case "GET":
			srv.GetJob(w, r)
		case "DELETE":
			srv.DeleteJob(w, r)
		default:
			http.Error(w, "only GET is allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc(srv.route("/gcodes/"), func(w http.ResponseWriter, r *http.Request) {
		// the only operation allowed on a gcode resource is to get the gcode
		// content for a job.
		switch r.Method {
		case "GET":
			srv.GetGCode(w, r)
		default:
			http.Error(w, "only GET is allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc(srv.route("/meshes/"), func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			srv.GetMesh(w, r)
		default:
			http.Error(w, "only GET is allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc(srv.route("/presets/"), func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			srv.GetPresets(w, r)
		default:
			http.Error(w, "only GET is allowed", http.StatusMethodNotAllowed)
		}
	})

	return mux
}

// path is a simple helper for constructing url paths by appending suffix to
// srv.Prefix.
func (srv *SnuggieServer) route(suffix string) string {
	return srv.Prefix + suffix
}

// trimPath trims the routing prefix from path and returns the suffix and the
// routing prefix.  The route must end in a slash '/'.  If path does not match
// the route an empty prefix is returned.
func (srv *SnuggieServer) trimPath(path, route string) (suffix, prefix string) {
	if !strings.HasSuffix(route, "/") {
		return "", ""
	}
	prefix = srv.route(route)
	suffix = strings.TrimPrefix(path, prefix)
	if len(suffix) == len(path) {
		return "", ""
	}
	return suffix, prefix
}

func (srv *SnuggieServer) GetGCode(w http.ResponseWriter, r *http.Request) {
	id, _ := srv.trimPath(r.URL.Path, "/gcodes/")
	path, err := ViewGCodeFile(id)
	if err != nil {
		http.Error(w, "unknown id", http.StatusNotFound)
		return
	}
	http.ServeFile(w, r, path)
}

func (srv *SnuggieServer) GetMesh(w http.ResponseWriter, r *http.Request) {
	id, _ := srv.trimPath(r.URL.Path, "/meshes/")
	path, err := ViewGCodeFile(id)
	if err != nil {
		http.Error(w, "unknown id", http.StatusNotFound)
		return
	}
	http.ServeFile(w, r, path)
}

func (srv *SnuggieServer) GetPresets(w http.ResponseWriter, r *http.Request) {
	id, _ := srv.trimPath(r.URL.Path, "/presets/")
	log.Println(id)
	if id != "slic3r" {
		http.Error(w, "only slic3r is supported at this time", http.StatusNotFound)
		return
	}
	var presetKeys []string
	for k := range srv.Slic3rPresets {
		presetKeys = append(presetKeys, k)
	}
	presets := &slicerjob.SlicerPreset{
		Slicer:  "slic3r",
		Presets: presetKeys,
	}
	jsonPresets, err := json.Marshal(presets)
	if err != nil {
		http.Error(w, "slic3r presets json error", http.StatusInternalServerError)
		return
	}
	w.Write(jsonPresets)
}

func (srv *SnuggieServer) ListJobs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var limit int
	var err error
	if limstr := q.Get("limit"); limstr != "" {
		limit, err = strconv.Atoi(limstr)
		if err != nil {
			http.Error(w, "limit: "+err.Error(), http.StatusBadRequest)
			return
		}
		if limit <= 0 {
			http.Error(w, "limit: not a positive number", http.StatusBadRequest)
			return
		}
	}

	var cursor []byte
	if curstr := q.Get("cursor"); curstr != "" {
		var err error
		cursor, err = base64.URLEncoding.DecodeString(curstr)
		if err != nil {
			http.Error(w, "cursor: invalid cursor", http.StatusBadRequest)
			return
		}
	}

	var filters []func(job *slicerjob.Job) error
	var status slicerjob.Status
	if statstr := q.Get("status"); statstr != "" {
		status, err = slicerjob.ParseStatus(statstr)
		if err != nil {
			http.Error(w, "status: "+err.Error(), http.StatusBadRequest)
			return
		}
		filters = append(filters, func(job *slicerjob.Job) error {
			if job.Status != status {
				return ErrSkip
			}
			return nil
		})
	}

	filter := func(job *slicerjob.Job) error {
		for _, fn := range filters {
			err := fn(job)
			if err != nil {
				return err
			}
		}
		return nil
	}
	jobs, cursor, err := ListJobs(100*time.Millisecond, limit, cursor, filter)
	if err == ErrExceededMaxDur {
		err = nil
	} else if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	page := slicerjob.JobPage(cursor, jobs)
	err = json.NewEncoder(w).Encode(page)
	if err != nil {
		log.Printf("encode: %v", err)
	}
}

func (srv *SnuggieServer) GetJob(w http.ResponseWriter, r *http.Request) {
	id, _ := srv.trimPath(r.URL.Path, "/jobs/")
	job, err := srv.lookupJob(id)
	if err != nil {
		http.Error(w, "lookup: "+err.Error(), http.StatusBadRequest)
		return
	}
	err = json.NewEncoder(w).Encode(job)
	if err != nil {
		log.Printf("http response: %v", err)
	}
}

func (srv *SnuggieServer) CreateJob(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	slicerBackend := r.FormValue("slicer")
	if slicerBackend != "slic3r" {
		http.Error(w, "slicer not supported", http.StatusBadRequest)
		return
	}
	var presets []string
	for p := range srv.Slic3rPresets {
		presets = append(presets, p)
	}

	preset := r.FormValue("preset")
	if preset == "" {
		http.Error(w, "invalid preset: must be one of ["+strings.Join(presets, " ")+"]", http.StatusBadRequest)
		return
	}
	if path := srv.Slic3rPresets[preset]; path == "" {
		http.Error(w, "unknown preset: must be one of ["+strings.Join(presets, " ")+"]", http.StatusBadRequest)
		return
	}

	//TODO make sure meshfile is at least .stl
	meshfile, fileheader, err := r.FormFile("meshfile")
	if err != nil {
		http.Error(w, "bad meshfile, or 'meshfile' field not present", http.StatusBadRequest)
		return
	}

	job, err := srv.registerJob(meshfile, fileheader, slicerBackend, preset)
	if err != nil {
		// TODO: distinguish unknown preset (Bad Request) from backend failure.
		http.Error(w, "registration failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jsonJob, err := json.Marshal(job)
	if err != nil {
		http.Error(w, "json didn't encode properly...Derp?\n"+err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	w.Write(jsonJob)
}

func (srv *SnuggieServer) registerJob(meshfile multipart.File, header *multipart.FileHeader, slicerBackend string, preset string) (*slicerjob.Job, error) {
	job := slicerjob.New()

	//do stuff to the job.
	job.Status = slicerjob.Accepted
	job.Progress = 0.0
	job.URL = srv.url("/jobs/" + job.ID)

	// if DataDir is empty the file will be in the working directory.
	ext := filepath.Ext(header.Filename)
	path := filepath.Join(srv.DataDir, job.ID+ext)
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("meshfile create: %v", err)
	}
	_, err = io.Copy(f, meshfile)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("meshfile write: %v", err)
	}

	err = PutMeshFile(job.ID, path)
	if err != nil {
		return nil, fmt.Errorf("meshfile: %v", err)
	}

	err = PutJob(job.ID, job)
	if err != nil {
		return nil, err
	}

	url := srv.url("/meshes/" + job.ID)
	if srv.LocalConsumer {
		url = "file://" + path
	}
	err = srv.S.ScheduleSliceJob(job.ID, url, slicerBackend, preset)
	if err != nil {
		os.Remove(path)
		DeleteJob(job.ID)
		return nil, err
	}

	return job, nil
}

func (srv *SnuggieServer) lookupJob(id string) (*slicerjob.Job, error) {
	job, err := ViewJob(id)
	if err != nil {
		err := fmt.Errorf("Job not found with id: %v", id)
		return nil, err
	}

	if err != nil {
		err := fmt.Errorf("json unmarshal problem: %v", id)
		return nil, err
	}
	return job, nil
}

func (srv *SnuggieServer) DeleteJob(w http.ResponseWriter, r *http.Request) {
	id, _ := srv.trimPath(r.URL.Path, "/jobs/")
	_, err := srv.lookupJob(id)
	if err != nil {
		http.Error(w, "lookup: "+err.Error(), http.StatusNotFound)
		return
	}
	srv.S.CancelSliceJob(id)
	CancelJob(id)

	if err != nil {
		log.Printf("http response: %v", err)
	}
}

func (srv *SnuggieServer) url(pathquery string) string {
	return srv.BaseURL + srv.Prefix + pathquery
}

// JobDone stores the location of the successful output g-code for job id
func (srv *SnuggieServer) JobDone(id, path string, err error) {
	if err != nil {
		log.Printf("FIXME -- failed job:%v err:%v", id, err)
		return
	}

	now := time.Now()

	err = PutGCodeFile(id, path)
	if err != nil {
		log.Printf("can't put gcode file path into database: %v", err)
		return
	}

	job, err := ViewJob(id)
	if err != nil {
		log.Printf("Can't view job from database:%v err:%v", id, err)
		return
	}
	job.Status = slicerjob.Complete
	job.GCodeURL = srv.url("/gcodes/" + id)
	job.Progress = 1.0
	job.Updated = &now
	job.Terminated = &now

	err = PutJob(id, job)
	if err != nil {
		log.Printf("Can't put job to database:%v err:%v", id, err)
		return
	}

	log.Printf("completed job:%v gcode:%v", id, path)
}

// RunConsumers pops jobs off the queue, fetches remote mesh files, slices
// them, and makes the resulting gcode accessible over HTTP,
func (srv *SnuggieServer) RunConsumer() {
	for {
		job, err := srv.C.NextSliceJob()
		if err != nil {
			log.Printf("consumer: %v", err)
			return
		}
		job.Done(srv.runConsumerJob(job))
	}
}

func (srv *SnuggieServer) runConsumerJob(job *Job) (path string, err error) {
	if !strings.HasPrefix(job.MeshURL, "file://") {
		return "", fmt.Errorf("consumer cannot process: %v", job.MeshURL)
	}

	gcode := filepath.Join(srv.DataDir, job.ID+".gcode")
	configPath := srv.Slic3rPresets[job.Preset]
	if configPath == "" {
		return "", fmt.Errorf("consumer: unknown preset")
	}
	slic3r := &Slic3r{
		Bin:        srv.Slic3r,
		ConfigPath: configPath,
		InPath:     strings.TrimPrefix(job.MeshURL, "file://"),
		OutPath:    gcode,
	}
	err = Run(slic3r, job.Cancel)
	if err != nil {
		return "", fmt.Errorf("run: %v", err)
	}
	_, err = os.Stat(slic3r.OutPath)
	if err != nil {
		return "", fmt.Errorf("stat gcode: %v", err)
	}
	return gcode, nil
}

func main() {
	machineID := flag.String("name", "snuggied0", "machine name for clustering")
	slic3rBin := flag.String("slic3r.bin", "", "specify slic3r location")
	slic3rConfigDir := flag.String("slic3r.configs", ".", "specify a directory with slic3r preset configurations")
	dataDir := flag.String("data", "", "location for database, .stl, .gcode")
	httpAddr := flag.String("http", ":8888", "address to serve traffic")
	baseURL := flag.String("baseurl", "", "links and redirection go to the specified base url")
	flagenv.Prefix = "SNUGGIED_"
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s [flags]\n", os.Args[0])
		fmt.Fprintln(os.Stderr)
		fmt.Fprintf(os.Stderr, "flags are:\n")
		flag.PrintDefaults()
		fmt.Fprintln(os.Stderr)
		fmt.Fprintf(os.Stderr, "%s can also be configured though the environment. for example\n", os.Args[0])
		fmt.Fprintln(os.Stderr)
		fmt.Fprintf(os.Stderr, "  SNUGGIED_SLIC3R_CONFIGS=./testdata/ %s", os.Args[0])
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "instead of")
		fmt.Fprintln(os.Stderr)

		fmt.Fprintf(os.Stderr, "  %s -slic3r.configs=./testdata/\n", os.Args[0])
		fmt.Fprintln(os.Stderr)

	}
	flagenv.Parse()
	flag.Parse()

	pathPrefix := "/slicer"
	if *baseURL != "" {
		u, err := url.Parse(*baseURL)
		if err != nil {
			log.Fatalf("baseurl: %v", err)
		}
		pathPrefix = strings.TrimSuffix(u.Path, "/")
	} else {
		urlHostPort := *httpAddr
		if strings.HasPrefix(urlHostPort, ":") {
			urlHostPort = "localhost" + urlHostPort
		}
		*baseURL = "http://" + urlHostPort
	}

	if *dataDir == "" {
		home := os.Getenv("HOME")
		if home == "" {
			log.Fatalf("data: unable to locate home directory")
		}
		*dataDir = filepath.Join(home, ".local", "share", "matching-snuggies", "data")
		err := os.MkdirAll(*dataDir, 0755)
		if err != nil {
			log.Fatalf("data: %v", err)
		}
	}

	// make sure that dataDir is a directory and that it's path is absolute.
	// forcing absolute paths is merely a simple way to prevent weird bugs
	// later on.
	err := pathIsDir(*dataDir)
	if err != nil {
		log.Fatalf("data directory: %v", err)
	}
	if !filepath.IsAbs(*dataDir) {
		log.Fatalf("data directory is not an absolute path: %v", *dataDir)
	}

	slic3rPresets, err := ReadPresetsDirSlic3r(*slic3rConfigDir)
	if err != nil {
		log.Fatalf("slic3r configs: %v", err)
	}
	if len(slic3rPresets) == 0 {
		log.Fatalf("slic3r configs: no presets found")
	}

	DB = loadDB(filepath.Join(*dataDir, "snuggied.boltdb"))
	fileroot := filepath.Join(*dataDir, "snuggied-files")
	err = os.MkdirAll(fileroot, 0750)
	if err != nil {
		log.Fatal(err)
	}

	srv := &SnuggieServer{
		BaseURL:       *baseURL,
		Prefix:        pathPrefix,
		DataDir:       fileroot,
		Slic3r:        *slic3rBin,
		Slic3rPresets: slic3rPresets,
	}

	// register http handlers
	srv.RegisterHandlers(http.DefaultServeMux)

	// the scheduler/consumer for the server are implemented using an in-memory
	// queue.
	memq := MemoryQueue(srv.JobDone)
	srv.S, srv.C = memq, memq
	srv.LocalConsumer = true // use file:// locations instead of http://

	// BUG:
	// there is a race condition starting the queue consumer before serving
	// http traffic. slice jobs could be finished before the http server is
	// capable of serving the result. this would be most problematic if binding
	// the address fails.
	go srv.RunConsumer()

	// run the garbage collector every minute, deleting objects which are more
	// than one hour old.
	gctrigger := make(chan struct{}, 1)
	gctrigger <- struct{}{}
	go gcLoop(time.Minute, 5*time.Minute, gctrigger)

	log.Printf("machine %s binding to %s", *machineID, *httpAddr)
	log.Fatal(http.ListenAndServe(*httpAddr, nil))
}

func gcLoop(delay, staleness time.Duration, trigger <-chan struct{}) {
	ticker := time.NewTicker(delay)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
		case <-trigger:
		}
		err := RemoveFiles(delay/2, 1000)
		if err != nil {
			log.Printf("remove: %v", err)
			// don't do anything special about errors removing files the
			// logging is specific enough for the user to handle anything.
		}
		err = DeleteOldJobs(time.Now().Add(-staleness), delay/2, 1000)
		if err != nil {
			log.Printf("gc: %v", err)
		}
	}
}

func pathIsDir(path string) error {
	stat, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("data directory: %v", err)
	}
	if !stat.IsDir() {
		return fmt.Errorf("data path is a not directory: %v", err)
	}
	return nil
}
