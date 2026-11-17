# Changelog

## [0.2.1](https://github.com/basisbit/karaoke-launcher/compare/v0.2.0...v0.2.1) (2026-11-17)


### Bug fixes

* bring the open window to the front on a second start ([1d9357a](https://github.com/basisbit/karaoke-launcher/commit/1d9357a5815f090447ebe66f45f04b528eceaf93))
* keep the game's keep list and archives when merging definitions ([52f9059](https://github.com/basisbit/karaoke-launcher/commit/52f90593afe59c3fe91135b93ee903d3e8c9ee99))
* never apply a stale or incomplete staged update ([1e8b103](https://github.com/basisbit/karaoke-launcher/commit/1e8b10332fb567817af284be8de4dc98e71e9ce6))
* pause downloads for program installs ([e5dde86](https://github.com/basisbit/karaoke-launcher/commit/e5dde86abe4eb31de09ec67a0b59580cfe4f2cbd))
* refuse archive names Windows does not allow ([05c8a36](https://github.com/basisbit/karaoke-launcher/commit/05c8a36be337d0d9ed3a618cce47d30708479ecd))
* require folders to start with a placeholder ([b2cc224](https://github.com/basisbit/karaoke-launcher/commit/b2cc22421ab76f1b8253ace1d72b13f2b7037d7c))
* settings apply the licence filter, keep the screen and retitle the window ([1e05489](https://github.com/basisbit/karaoke-launcher/commit/1e054897bf8acf515b3ac92e8660ec73901d698b))
* setup keeps game settings and removes old files ([917aad1](https://github.com/basisbit/karaoke-launcher/commit/917aad122ee19f8affdbb647eabcc442cede54a1))
* songs screen keeps ticks and removes only the confirmed pack ([7609aa2](https://github.com/basisbit/karaoke-launcher/commit/7609aa21c9dc570fa80080c02bc12f99d4a9c753))


### Documentation

* behaviour specification of the launcher window ([61d4657](https://github.com/basisbit/karaoke-launcher/commit/61d46576273e80e02004b2242256c4c06d613f89))
* pin examples to launcher v0.2.0, formats will change ([a3ed02a](https://github.com/basisbit/karaoke-launcher/commit/a3ed02a268671f5d94272777d33d2f8ff90c6588))


### Tests

* accept CRLF in the setup script check ([4090ecb](https://github.com/basisbit/karaoke-launcher/commit/4090ecbe9cdd589a8e64796562b800c51a8f45d1))
* wait for the songs list before cleanup ([6e986e9](https://github.com/basisbit/karaoke-launcher/commit/6e986e966c54473d70d0b7c2ff23150379f80b77))

## [0.2.0](https://github.com/basisbit/karaoke-launcher/compare/v0.1.0...v0.2.0) (2026-11-16)


### Features

* bundle tool draft for Flatpak ([ecc57d4](https://github.com/basisbit/karaoke-launcher/commit/ecc57d452f3bcc2c8176066adc28bc6bec0085a4))
* bundle tool for macOS ([1547fb1](https://github.com/basisbit/karaoke-launcher/commit/1547fb1c0ab633453660ea6ec38bd361dab61004))
* bundle tool for Windows ([00326cb](https://github.com/basisbit/karaoke-launcher/commit/00326cbee4d000aa321d997ec701fcc2a322964c))
* support bundling with UltraStar Deluxe ([eb74cde](https://github.com/basisbit/karaoke-launcher/commit/eb74cde7a8a2886b4eadc7db380fe0a2d7a511e1))


### Documentation

* bundles and publishing ([3fed99d](https://github.com/basisbit/karaoke-launcher/commit/3fed99d785951a427ed763832e8152af24d6916a))


### Code refactoring

* **bundle:** build AppStream metainfo with encoding/xml ([3b15640](https://github.com/basisbit/karaoke-launcher/commit/3b156409247e798a62cb99819ddc54e481d90154))


### Tests

* accept Windows link targets in TestSiteFlatpak ([228c463](https://github.com/basisbit/karaoke-launcher/commit/228c46340b29f9a964354b07217920f2e221844e))
* **bundle:** expect the .dmg when hdiutil exists ([03a977e](https://github.com/basisbit/karaoke-launcher/commit/03a977e52a94db4b4d3be596e97729407e114073))


### Build and dependencies

* **deps:** bump the go-major-minor group across 1 directory with 2 updates ([5cf8419](https://github.com/basisbit/karaoke-launcher/commit/5cf841947f358fcc66aa7894c509b7d3d13d8f77))
* **deps:** regenerate licence texts ([77fb9ff](https://github.com/basisbit/karaoke-launcher/commit/77fb9ff4130a32cdcb959026b07f24f810ca7874))
* leave module versions out of licenses.txt ([2004e7a](https://github.com/basisbit/karaoke-launcher/commit/2004e7a442242b450a1ab6bdf4dd6eb85b11946f))


### CI

* show all commit types in release notes ([c926b93](https://github.com/basisbit/karaoke-launcher/commit/c926b93724e6f3aafd37b8e31ede4a8f8e1ad182))

## 0.1.0 (2026-11-15)


### Features

* first version of the karaoke launcher ([9094565](https://github.com/basisbit/karaoke-launcher/commit/90945658082f2d3eb4ae4555c411c1ac210cba04))
* signed program updates and themes from feeds ([d86faaa](https://github.com/basisbit/karaoke-launcher/commit/d86faaa12c6dc13d6b714e2ee80ad6dfa307d77d))


### Bug fixes

* **deps:** update golang.org/x/image to v0.43.0 (GO-2026-5062) ([a7da0fa](https://github.com/basisbit/karaoke-launcher/commit/a7da0fab0a06340c12148a680fd0c7c6db671d2a))
* resolve complaints from PR review ([dc3a29c](https://github.com/basisbit/karaoke-launcher/commit/dc3a29cc92ee4d5694a567313d83a34e4454cd10))
* resolve staticcheck findings ([79081a3](https://github.com/basisbit/karaoke-launcher/commit/79081a31bc74555812af2489f898246b15f1dbbd))
