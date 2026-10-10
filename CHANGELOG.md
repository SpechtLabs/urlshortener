# Changelog

## [0.1.2](https://github.com/SpechtLabs/urlshortener/compare/v0.1.1...v0.1.2) (2026-10-10)


### Bug Fixes

* **api:** list no shortlinks for a user who owns none, rather than failing ([f82c373](https://github.com/SpechtLabs/urlshortener/commit/f82c3736b128a597e6055b53d6001ba74676569a))
* **controller:** update an existing redirect Ingress instead of failing on it ([f82c373](https://github.com/SpechtLabs/urlshortener/commit/f82c3736b128a597e6055b53d6001ba74676569a))
* **deps:** update go modules ([#189](https://github.com/SpechtLabs/urlshortener/issues/189)) ([68a4429](https://github.com/SpechtLabs/urlshortener/commit/68a442916f1d792ea142ac872feea7ddf34cfb86))
* **deps:** update Go modules, Kubernetes 0.37 and controller-runtime 0.25 included ([#183](https://github.com/SpechtLabs/urlshortener/issues/183)) ([2c6039b](https://github.com/SpechtLabs/urlshortener/commit/2c6039bd5b9afdf1f3d0df5b272f5feab0e3cdd2))
* **deps:** update module github.com/gin-contrib/zap to v1.1.6 ([#164](https://github.com/SpechtLabs/urlshortener/issues/164)) ([f83131c](https://github.com/SpechtLabs/urlshortener/commit/f83131c650c06d4a3c9e12c7b5b2d0e35605c9da))
* **deps:** update module github.com/onsi/ginkgo/v2 to v2.33.1 ([#196](https://github.com/SpechtLabs/urlshortener/issues/196)) ([cd1dac8](https://github.com/SpechtLabs/urlshortener/commit/cd1dac8e1f4cd2a0e98080352b45d5e2f3585a13))
* **deps:** update module github.com/prometheus/client_golang to v1.25.0 ([#194](https://github.com/SpechtLabs/urlshortener/issues/194)) ([234e79a](https://github.com/SpechtLabs/urlshortener/commit/234e79a4263b1b7a70f9864a14d360d1cba3f21a))
* **deps:** update module github.com/spechtlabs/go-otel-utils/otelprovider to v0.0.15 ([#153](https://github.com/SpechtLabs/urlshortener/issues/153)) ([2779dc0](https://github.com/SpechtLabs/urlshortener/commit/2779dc0386c5e1195e39a387f656f681edc2bfc6))
* **deps:** update module github.com/spechtlabs/go-otel-utils/otelzap to v0.0.15 ([#160](https://github.com/SpechtLabs/urlshortener/issues/160)) ([5f561ac](https://github.com/SpechtLabs/urlshortener/commit/5f561ac3a6dbf4b31638c51ac6febb56f23d3a79))
* **deps:** update module github.com/swaggo/gin-swagger to v1.6.1 ([#161](https://github.com/SpechtLabs/urlshortener/issues/161)) ([b5ee65f](https://github.com/SpechtLabs/urlshortener/commit/b5ee65fcd55cff09351e1fd9e46705f01ee8fb13))
* serve shortlinks and the API alongside the controller, on --bind-address ([f82c373](https://github.com/SpechtLabs/urlshortener/commit/f82c3736b128a597e6055b53d6001ba74676569a))
