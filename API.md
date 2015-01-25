#API Documentation

##Jobs

**POST /slicer/jobs**

```
$ curl http://localhost:8888/slicer/jobs -F slicer=slic3r -F preset=hq -F meshfile=@testdata/FirstCube.stl
{
    "id":"e2df75e4-714d-408a-924b-9284bf41a533",
    "status":"accepted",
    "progress":0,
    "url":"http://localhost:8888/slicer/jobs/e2df75e4-714d-408a-924b-9284bf41a533",
    "gcode_url":""
}
```

Slice an STL file.

**GET /slicer/jobs/:id**

```
$ curl http://localhost:8888/slicer/jobs/e2df75e4-714d-408a-924b-9284bf41a533
{
    "id":"e2df75e4-714d-408a-924b-9284bf41a533",
    "status":"complete",
    "progress":1,
    "url":"http://localhost:8888/slicer/jobs/e2df75e4-714d-408a-924b-9284bf41a533",
    "gcode_url":"http://localhost:8888/slicer/gcodes/e2df75e4-714d-408a-924b-9284bf41a533"
}
```

Get the status of a slicing job.

**DELETE /slicer/jobs/:id**

Cancel a slicing job.

##Meshes

**GET /slicer/meshes/:id**

Fetch mesh file(s) corresponding to job :id.

##GCodes

**GET /slicer/gcodes/:id**

Fetch the g-code file produced by job :id.

```
$ curl http://localhost:8888/slicer/gcodes/e2df75e4-714d-408a-924b-9284bf41a533
; generated by Slic3r 1.1.7 on 2015-01-23 at 23:48:20

; perimeters extrusion width = 0.44mm
; infill extrusion width = 0.44mm
; solid infill extrusion width = 0.44mm
; top infill extrusion width = 0.44mm

G21 ; set units to millimeters
M107
M104 S195 ; set temperature
; ...
```