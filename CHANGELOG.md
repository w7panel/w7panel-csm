# Changelog

- Add common `build-helm`, `build-image`, `push-zpk`, and `full` Make targets so one explicit version builds and publishes matching release artifacts.

- Add Makefile targets for Go builds, container images, CRDs, and Helm workflows.
- Initialize the first Ubuntu operating system template on startup, boot hosts with systemd, and grant access to tenant SSH key ConfigMaps.
- Add CSM hosts backed by Sysbox and PVCs, system templates, SSH access, and a browser terminal.
- Add Shell preinstallation, system upgrades, and execution tracking.
- Add W7 payment orders for host purchase, renewal, expansion, and data disks, with server-side quotes and payment verification.
- Add CSM APIs, Vue management UI, Kubernetes CRDs, container builds, and a Helm chart.
- Initialize the independent CSM Go and Vue project so parallel feature work starts from one buildable baseline.
