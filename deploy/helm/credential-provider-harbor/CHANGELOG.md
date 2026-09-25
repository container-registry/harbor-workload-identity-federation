# Changelog

## [0.2.1](https://github.com/container-registry/harbor-workload-identity-federation/compare/chart-v0.2.0...chart-v0.2.1) (2026-09-25)


### Bug Fixes

* **chart:** Install the v0.1.2 deployer by default ([e107e1d](https://github.com/container-registry/harbor-workload-identity-federation/commit/e107e1dd578593131797364e71effe37f4c0c4de))
* **chart:** Publish a chart whose default image is the released one ([#37](https://github.com/container-registry/harbor-workload-identity-federation/issues/37)) ([9a5e3b0](https://github.com/container-registry/harbor-workload-identity-federation/commit/9a5e3b00eac847b78aa50b26e19becde7a4fcfd8))

## [0.2.0](https://github.com/container-registry/harbor-workload-identity-federation/compare/chart-v0.1.1...chart-v0.2.0) (2026-09-25)


### Features

* **installer:** Wire kubelet on AKS, RKE2, and MicroK8s ([#18](https://github.com/container-registry/harbor-workload-identity-federation/issues/18)) ([7295be7](https://github.com/container-registry/harbor-workload-identity-federation/commit/7295be7ee2946b1b14ceae219e508ccfb41ab7b8))


### Bug Fixes

* Add optional kind kubelet override ([57d5727](https://github.com/container-registry/harbor-workload-identity-federation/commit/57d5727cec73c5da2081ed8266de077fbcb0165f))
* Align Helm app version with deployer tag ([ca81686](https://github.com/container-registry/harbor-workload-identity-federation/commit/ca81686d32f3f68cda991b51f69274ec40d150e1)), closes [#5](https://github.com/container-registry/harbor-workload-identity-federation/issues/5)
* **chart:** Harden the DaemonSet and validate values at install time ([#14](https://github.com/container-registry/harbor-workload-identity-federation/issues/14)) ([e5945d1](https://github.com/container-registry/harbor-workload-identity-federation/commit/e5945d195694658c04ef0d9f19af858822d92160))
* **release:** Bump the chart's appVersion, not its version ([#30](https://github.com/container-registry/harbor-workload-identity-federation/issues/30)) ([9dd058b](https://github.com/container-registry/harbor-workload-identity-federation/commit/9dd058b9b0e00a67dd6bb740878269e8885c6927))
* Restart the right rke2 unit, and stop asking for --api-audiences ([#27](https://github.com/container-registry/harbor-workload-identity-federation/issues/27)) ([784be99](https://github.com/container-registry/harbor-workload-identity-federation/commit/784be99b4d5fee98fee808b56008f9d0888fa6de))
* Use SemVer app version in Helm chart ([#8](https://github.com/container-registry/harbor-workload-identity-federation/issues/8)) ([9746e82](https://github.com/container-registry/harbor-workload-identity-federation/commit/9746e8206d12988cc7cca8db3db237f8a3926429))


### Documentation

* Call the Harbor side Trusted Issuers ([#16](https://github.com/container-registry/harbor-workload-identity-federation/issues/16)) ([14cd8c8](https://github.com/container-registry/harbor-workload-identity-federation/commit/14cd8c89b97e56f99693e1de858ec131bfb2cc24))
* Rename Workload Identity Federation to Federated Robot Accounts ([#10](https://github.com/container-registry/harbor-workload-identity-federation/issues/10)) ([8c8c995](https://github.com/container-registry/harbor-workload-identity-federation/commit/8c8c995e4306e3f888b699cada4fefd5a7615839))
