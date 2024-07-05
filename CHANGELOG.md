# Changelog


- - -
## v2.1.0 - 2024-07-05
#### Build system
- fix failing clean job [ci skip] - (54e9fad) - Massimo Gengarelli
- change the trigger for bitbucket [ci skip] - (5e73828) - Massimo Gengarelli
#### Continuous Integration
- fix cog configuration for GitHub - (6dfa6b2) - Massimo Gengarelli
- automatic creation of release - (2401430) - Massimo Gengarelli
#### Documentation
- fix the dates in the changelog [ci skip] - (c54b8ce) - Massimo Gengarelli
- include CHANGELOG.md file - (be0704a) - Massimo Gengarelli
#### Features
- **(monitoring)** instrument code using Prometheus - (704201a) - Massimo Gengarelli
- **(watcher)** implement configurable behavior for the nswatcher - (a07be7f) - Massimo Gengarelli
#### Miscellaneous Chores
- bump version in Makefile - (41dea50) - Massimo Gengarelli
- include LICENSE file - (a1d0fb9) - Massimo Gengarelli
- configuration file for cocogitto [ci skip] - (9eaf5ab) - Massimo Gengarelli
- make it easy to deploy a very basic monitoring stack - (ee59b85) - Massimo Gengarelli

- - -


## v2.0.4 - 2024-07-02
#### Bug Fixes
- correctly force stop the DeploymentWatcher - (ec20449) - Massimo Gengarelli
- change default timeouts - (219fcdf) - Massimo Gengarelli
#### Features
- **(restart)** implement restart strategy for CRD Watcher - (7d150f7) - Massimo Gengarelli
- **(restart)** restart NSWatcher when timeout expires - (b0aa7ca) - Massimo Gengarelli
- make it possible to hot change the `podType` - (26b1783) - Massimo Gengarelli
- force stop deployment watcher - (70446cf) - Massimo Gengarelli
- restart pod watcher when timeout expires - (82c19fd) - Massimo Gengarelli
#### Miscellaneous Chores
- bump version - (d392305) - Massimo Gengarelli
- add .vscode tasks [ci skip] - (ad9510d) - Massimo Gengarelli
#### Style
- change logs for PodWatcher - (cc55d08) - Massimo Gengarelli
- change logs for deployment - (0f3068b) - Massimo Gengarelli
- change logs for CRD Watcher - (b3d4045) - Massimo Gengarelli
- change logs for NamespaceWatcher - (980e43e) - Massimo Gengarelli
- change logs for `main` function - (368c145) - Massimo Gengarelli
- rename methods - (297637e) - Massimo Gengarelli
#### Tests
- better automated e2e tests - (a6ba5d0) - Massimo Gengarelli
- minor modification to the e2e test - (e6e6ee1) - Massimo Gengarelli

- - -

## v2.0.3 - 2024-06-29
#### Bug Fixes
- not a real fix, simply extend the timeout to 24 hours - (ab03319) - Massimo Gengarelli

- - -

## v2.0.2 - 2024-06-29
#### Bug Fixes
- do not panic when receiving nil in events - (4b3d8ee) - Massimo Gengarelli
- bump version - (d138eef) - Massimo Gengarelli

- - -

## v2.0.1 - 2024-06-28
#### Documentation
- **(architecture)** fix image of architecture - (618cbd6) - Massimo Gengarelli
- fix documentation for clusterrole [ci skip] - (5a8131c) - Massimo Gengarelli
#### Features
- **(pod)** pod watcher now sends events - (9817a84) - Massimo Gengarelli

- - -

## v2.0.0 - 2024-06-28
#### Features
- honor the enabled flag - (94e3037) - Massimo Gengarelli
- implement a new strategy for generating Chaos - (c5bee05) - Massimo Gengarelli

- - -

## v1.1.0 - 2024-06-27
#### Bug Fixes
- actually use the environment variable - (23156d9) - Massimo Gengarelli
#### Build system
- deploy on dockerhub - (0e3453c) - Massimo Gengarelli
#### Features
- **(log)** add some more useful logs for debugging - (fc7a171) - Massimo Gengarelli
- parse loglevel from environment variable - (fdad2b2) - Massimo Gengarelli
#### Tests
- use debug loglevel in local tests - (1b18318) - Massimo Gengarelli

- - -

## v1.0.0 - 2024-06-27
#### Build system
- version binary and docker image - (76da64c) - Massimo Gengarelli
- fix bitbucket pipeline - (4eddd7e) - Massimo Gengarelli
- integrate bitbucket-pipelines - (67d2be5) - Massimo Gengarelli
- add github action (ci only) - (6bb1a8d) - Massimo Gengarelli
#### Miscellaneous Chores
- first commit - (b4fbb09) - Massimo Gengarelli
#### Refactoring
- clean code - (18f0f0c) - Massimo Gengarelli
- split watchers into packages and create factories - (3c0423b) - Massimo Gengarelli


