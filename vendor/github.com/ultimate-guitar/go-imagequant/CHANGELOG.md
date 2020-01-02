#### 2018-06-23 2.12.1.0
* Bump libimagequant version
* Add lib building with go get
* Disable sudo for travis builds

#### 2018-03-18 2.11.10.2
* Small fixes
* Add documentation

#### 2018-03-10 2.11.10.1
* Fix data race error created by prev commit

#### 2018-03-10 2.11.10.0
* Remove libpngquant sources from repository. Now go-imagequant use system version of libimagequant
* Add hight level function that works with bytes (see Optimize.go)
* Update cmd app
* Fix memory leak

#### 2017-03-03 2.9go1.1
* go-imagequant: Update bundled libimagequant from 2.8.0 to 2.9.0
* go-imagequant: Separate CGO_LDFLAGS for Linux and Windows targets
* gopngquant: Fix an issue with non-square images

#### 2016-11-24 2.8go1.0
 * Initial public release
