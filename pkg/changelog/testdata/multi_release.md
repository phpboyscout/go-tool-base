# v1.3.0

### Features

* **http:** add middleware chaining support
* **grpc:** add interceptor chaining support

### Bug Fixes

* **config:** fix hot-reload race condition
* resolve panic on nil logger

### Performance Improvements

* **cache:** reduce allocations in LRU eviction

# v1.2.0

### Features

* **chat:** add streaming response support

### Bug Fixes

* **vcs:** fix SSH agent auth fallback

### BREAKING CHANGES

* **config:** rename `ConfigPath` to `ConfigDir` across all packages

# v1.1.0

### Bug Fixes

* **setup:** fix update check timeout
* **forms:** handle terminal resize during prompt

### Other

* update go.mod dependencies to latest
