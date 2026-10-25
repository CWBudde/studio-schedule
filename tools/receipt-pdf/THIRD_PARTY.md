# Third-party components of receipt.wasm

`internal/pdf/typstwasm/receipt.wasm` is built from this crate (MIT, like the
repository) and the crates below, linked statically, and embeds the fonts in
`fonts/`. It is distributed with the server binary and the web demo.

## Fonts

- Libertinus Serif (Regular, Bold, Italic), from typst-assets 0.15.1:
  SIL Open Font License 1.1, see [fonts/OFL.txt](fonts/OFL.txt).

## Crates

typst and its crates are licensed under Apache-2.0
([LICENSE-APACHE-2.0.txt](LICENSE-APACHE-2.0.txt)). Every crate of
`Cargo.lock` with its license expression, as its `Cargo.toml` declares it
(some are only used on other targets or at build time):

| Crate                      | Version           | License                                             |
| -------------------------- | ----------------- | --------------------------------------------------- |
| adler2                     | 2.0.1             | 0BSD OR MIT OR Apache-2.0                           |
| aho-corasick               | 1.1.5             | Unlicense OR MIT                                    |
| approx                     | 0.5.1             | Apache-2.0                                          |
| ar_archive_writer          | 0.5.3             | Apache-2.0 WITH LLVM-exception                      |
| arrayref                   | 0.3.9             | BSD-2-Clause                                        |
| arrayvec                   | 0.7.8             | MIT OR Apache-2.0                                   |
| autocfg                    | 1.5.1             | Apache-2.0 OR MIT                                   |
| az                         | 1.3.0             | MIT/Apache-2.0                                      |
| base64                     | 0.22.1            | MIT OR Apache-2.0                                   |
| base64                     | 0.23.1            | MIT OR Apache-2.0                                   |
| biblatex                   | 0.12.0            | MIT OR Apache-2.0                                   |
| bincode                    | 1.3.3             | MIT                                                 |
| bit-set                    | 0.8.0             | Apache-2.0 OR MIT                                   |
| bit-vec                    | 0.8.0             | Apache-2.0 OR MIT                                   |
| bitflags                   | 2.13.2            | MIT OR Apache-2.0                                   |
| bumpalo                    | 3.20.3            | MIT OR Apache-2.0                                   |
| by_address                 | 1.2.1             | MIT OR Apache-2.0                                   |
| bytemuck                   | 1.25.2            | Zlib OR Apache-2.0 OR MIT                           |
| bytemuck_derive            | 1.12.1            | Zlib OR Apache-2.0 OR MIT                           |
| byteorder-lite             | 0.1.0             | Unlicense OR MIT                                    |
| cc                         | 1.10.0            | MIT OR Apache-2.0                                   |
| cfg-if                     | 1.0.5             | MIT OR Apache-2.0                                   |
| chinese-number             | 0.7.8             | MIT                                                 |
| chinese-variant            | 1.1.6             | MIT                                                 |
| ciborium                   | 0.2.2             | Apache-2.0                                          |
| ciborium-io                | 0.2.2             | Apache-2.0                                          |
| ciborium-ll                | 0.2.2             | Apache-2.0                                          |
| citationberg               | 0.7.0             | MIT OR Apache-2.0                                   |
| cobs                       | 0.3.0             | MIT OR Apache-2.0                                   |
| codex                      | 0.3.0             | Apache-2.0                                          |
| color                      | 0.3.3             | Apache-2.0 OR MIT                                   |
| color_quant                | 1.1.0             | MIT                                                 |
| comemo                     | 0.5.1             | MIT OR Apache-2.0                                   |
| comemo-macros              | 0.5.1             | MIT OR Apache-2.0                                   |
| core_maths                 | 0.1.1             | MIT                                                 |
| crc32fast                  | 1.5.3             | MIT OR Apache-2.0                                   |
| crossbeam-deque            | 0.8.8             | MIT OR Apache-2.0                                   |
| crossbeam-epoch            | 0.9.21            | MIT OR Apache-2.0                                   |
| crossbeam-utils            | 0.8.23            | MIT OR Apache-2.0                                   |
| crunchy                    | 0.2.4             | not built for wasm32-wasip1                         |
| csv                        | 1.4.0             | Unlicense/MIT                                       |
| csv-core                   | 0.1.13            | Unlicense/MIT                                       |
| data-url                   | 0.3.2             | MIT OR Apache-2.0                                   |
| deranged                   | 0.5.9             | MIT OR Apache-2.0                                   |
| displaydoc                 | 0.2.7             | MIT OR Apache-2.0                                   |
| ecow                       | 0.2.6             | MIT OR Apache-2.0                                   |
| either                     | 1.19.0            | MIT OR Apache-2.0                                   |
| enum-ordinalize            | 4.4.2             | MIT                                                 |
| enum-ordinalize-derive     | 4.4.2             | MIT                                                 |
| equivalent                 | 1.0.3             | Apache-2.0 OR MIT                                   |
| euclid                     | 0.22.14           | MIT OR Apache-2.0                                   |
| fancy-regex                | 0.16.2            | MIT                                                 |
| fastrand                   | 2.5.0             | Apache-2.0 OR MIT                                   |
| fdeflate                   | 0.3.7             | MIT OR Apache-2.0                                   |
| fearless_simd              | 0.4.1             | Apache-2.0 OR MIT                                   |
| fearless_simd              | 1.1.0             | Apache-2.0 OR MIT                                   |
| find-msvc-tools            | 0.1.15            | MIT OR Apache-2.0                                   |
| flate2                     | 1.1.10            | MIT OR Apache-2.0                                   |
| float-cmp                  | 0.9.0             | MIT                                                 |
| fnv                        | 1.0.7             | Apache-2.0 / MIT                                    |
| foldhash                   | 0.2.0             | Zlib                                                |
| font-types                 | 0.11.3            | MIT OR Apache-2.0                                   |
| fontconfig-parser          | 0.5.8             | not built for wasm32-wasip1                         |
| fontdb                     | 0.23.0            | MIT                                                 |
| form_urlencoded            | 1.2.2             | MIT OR Apache-2.0                                   |
| gif                        | 0.14.2            | MIT OR Apache-2.0                                   |
| glidesort                  | 0.1.2             | MIT OR Apache-2.0                                   |
| glifo                      | 0.1.1             | Apache-2.0 OR MIT                                   |
| guillotiere                | 0.7.0             | MIT/Apache-2.0                                      |
| half                       | 2.7.1             | MIT OR Apache-2.0                                   |
| hashbrown                  | 0.17.1            | MIT OR Apache-2.0                                   |
| hayagriva                  | 0.10.1            | MIT OR Apache-2.0                                   |
| hayro                      | 0.7.1             | Apache-2.0 OR MIT                                   |
| hayro-ccitt                | 0.3.0             | Apache-2.0 OR MIT                                   |
| hayro-ccitt                | 0.4.0             | Apache-2.0 OR MIT                                   |
| hayro-cmap                 | 0.1.0             | Apache-2.0 OR MIT                                   |
| hayro-interpret            | 0.7.0             | Apache-2.0 OR MIT                                   |
| hayro-jbig2                | 0.3.1             | Apache-2.0 OR MIT                                   |
| hayro-jpeg2000             | 0.3.5             | Apache-2.0 OR MIT                                   |
| hayro-postscript           | 0.1.0             | Apache-2.0 OR MIT                                   |
| hayro-svg                  | 0.7.0             | Apache-2.0 OR MIT                                   |
| hayro-syntax               | 0.7.2             | Apache-2.0 OR MIT                                   |
| hayro-write                | 0.7.0             | Apache-2.0 OR MIT                                   |
| heck                       | 0.5.0             | MIT OR Apache-2.0                                   |
| hypher                     | 0.1.8             | MIT OR Apache-2.0                                   |
| icu_collator               | 2.4.0             | Unicode-3.0                                         |
| icu_collator_data          | 2.4.0             | Unicode-3.0                                         |
| icu_collections            | 2.4.0             | Unicode-3.0                                         |
| icu_locale                 | 2.4.0             | Unicode-3.0                                         |
| icu_locale_core            | 2.4.0             | Unicode-3.0                                         |
| icu_locale_data            | 2.4.0             | Unicode-3.0                                         |
| icu_locale_fallback        | 2.4.0             | Unicode-3.0                                         |
| icu_locale_fallback_data   | 2.4.0             | Unicode-3.0                                         |
| icu_normalizer             | 2.4.0             | Unicode-3.0                                         |
| icu_normalizer_data        | 2.4.0             | Unicode-3.0                                         |
| icu_properties             | 2.4.0             | Unicode-3.0                                         |
| icu_properties_data        | 2.4.0             | Unicode-3.0                                         |
| icu_provider               | 2.4.0             | Unicode-3.0                                         |
| icu_provider_blob          | 2.4.0             | Unicode-3.0                                         |
| icu_segmenter              | 2.4.0             | Unicode-3.0                                         |
| icu_segmenter_data         | 2.4.0             | Unicode-3.0                                         |
| idna                       | 1.1.0             | MIT OR Apache-2.0                                   |
| idna_adapter               | 1.2.2             | Apache-2.0 OR MIT                                   |
| image                      | 0.25.10           | MIT OR Apache-2.0                                   |
| image-webp                 | 0.2.4             | MIT OR Apache-2.0                                   |
| imagesize                  | 0.14.0            | MIT                                                 |
| indexmap                   | 2.14.2            | Apache-2.0 OR MIT                                   |
| infer                      | 0.19.0            | MIT                                                 |
| itoa                       | 1.0.18            | MIT OR Apache-2.0                                   |
| kamadak-exif               | 0.6.1             | BSD-2-Clause                                        |
| krilla                     | 0.8.2             | MIT OR Apache-2.0                                   |
| krilla-svg                 | 0.8.1             | MIT OR Apache-2.0                                   |
| kurbo                      | 0.13.1            | Apache-2.0 OR MIT                                   |
| libc                       | 0.2.191           | MIT OR Apache-2.0                                   |
| libm                       | 0.2.16            | MIT                                                 |
| linebender_resource_handle | 0.1.1             | Apache-2.0 OR MIT                                   |
| linked-hash-map            | 0.5.6             | MIT/Apache-2.0                                      |
| lipsum                     | 0.9.1             | MIT                                                 |
| litemap                    | 0.8.3             | Unicode-3.0                                         |
| lock_api                   | 0.4.14            | MIT OR Apache-2.0                                   |
| log                        | 0.4.34            | MIT OR Apache-2.0                                   |
| memchr                     | 2.8.3             | Unlicense OR MIT                                    |
| memmap2                    | 0.9.12            | MIT OR Apache-2.0                                   |
| miniz_oxide                | 0.8.9             | MIT OR Zlib OR Apache-2.0                           |
| miniz_oxide                | 0.9.1             | MIT OR Zlib OR Apache-2.0                           |
| moxcms                     | 0.8.1             | BSD-3-Clause OR Apache-2.0                          |
| mutate_once                | 0.1.2             | BSD-2-Clause                                        |
| num-bigint                 | 0.4.8             | MIT OR Apache-2.0                                   |
| num-conv                   | 0.2.2             | MIT OR Apache-2.0                                   |
| num-integer                | 0.1.47            | MIT OR Apache-2.0                                   |
| num-traits                 | 0.2.19            | MIT OR Apache-2.0                                   |
| object                     | 0.39.1            | Apache-2.0 OR MIT                                   |
| once_cell                  | 1.21.4            | MIT OR Apache-2.0                                   |
| palette                    | 0.7.7             | MIT OR Apache-2.0                                   |
| palette_derive             | 0.7.7             | MIT OR Apache-2.0                                   |
| palette_math               | 0.7.7             | MIT OR Apache-2.0                                   |
| parking_lot                | 0.12.5            | MIT OR Apache-2.0                                   |
| parking_lot_core           | 0.9.12            | MIT OR Apache-2.0                                   |
| paste                      | 1.0.15            | MIT OR Apache-2.0                                   |
| pdf-writer                 | 0.15.0            | MIT OR Apache-2.0                                   |
| peniko                     | 0.6.1             | Apache-2.0 OR MIT                                   |
| percent-encoding           | 2.3.2             | MIT OR Apache-2.0                                   |
| phf                        | 0.13.1            | MIT                                                 |
| phf_generator              | 0.13.1            | MIT                                                 |
| phf_macros                 | 0.13.1            | MIT                                                 |
| phf_shared                 | 0.13.1            | MIT                                                 |
| pic-scale                  | 0.7.12            | BSD-3-Clause OR Apache-2.0                          |
| pico-args                  | 0.5.0             | MIT                                                 |
| plist                      | 1.10.3            | MIT                                                 |
| png                        | 0.18.1            | MIT OR Apache-2.0                                   |
| polycool                   | 0.4.0             | MIT OR Apache-2.0                                   |
| portable-atomic            | 1.15.0            | Apache-2.0 OR MIT                                   |
| postcard                   | 1.1.3             | MIT OR Apache-2.0                                   |
| potential_utf              | 0.1.6             | Unicode-3.0                                         |
| powerfmt                   | 0.2.1             | MIT OR Apache-2.0                                   |
| ppv-lite86                 | 0.2.21            | MIT OR Apache-2.0                                   |
| proc-macro-hack            | 0.5.20+deprecated | MIT OR Apache-2.0                                   |
| proc-macro2                | 1.0.107           | MIT OR Apache-2.0                                   |
| psm                        | 0.1.32            | MIT OR Apache-2.0                                   |
| pxfm                       | 0.1.30            | BSD-3-Clause OR Apache-2.0                          |
| quick-error                | 2.0.1             | MIT/Apache-2.0                                      |
| quick-xml                  | 0.38.4            | MIT                                                 |
| quick-xml                  | 0.42.0            | MIT                                                 |
| quote                      | 1.0.47            | MIT OR Apache-2.0                                   |
| rand                       | 0.8.8             | MIT OR Apache-2.0                                   |
| rand_chacha                | 0.3.1             | MIT OR Apache-2.0                                   |
| rand_core                  | 0.6.4             | MIT OR Apache-2.0                                   |
| rayon                      | 1.12.0            | MIT OR Apache-2.0                                   |
| rayon-core                 | 1.13.0            | MIT OR Apache-2.0                                   |
| read-fonts                 | 0.39.2            | MIT OR Apache-2.0                                   |
| redox_syscall              | 0.5.18            | not built for wasm32-wasip1                         |
| regex                      | 1.13.1            | MIT OR Apache-2.0                                   |
| regex-automata             | 0.4.18            | MIT OR Apache-2.0                                   |
| regex-syntax               | 0.8.11            | MIT OR Apache-2.0                                   |
| resvg                      | 0.47.0            | Apache-2.0 OR MIT                                   |
| rgb                        | 0.8.53            | MIT                                                 |
| roman-numerals-rs          | 3.1.0             | 0BSD OR CC0-1.0                                     |
| roxmltree                  | 0.20.0            | not built for wasm32-wasip1                         |
| roxmltree                  | 0.21.2            | MIT OR Apache-2.0                                   |
| rust_decimal               | 1.43.0            | MIT                                                 |
| rustc-hash                 | 2.1.3             | Apache-2.0 OR MIT                                   |
| rustybuzz                  | 0.20.1            | MIT                                                 |
| ryu                        | 1.0.23            | Apache-2.0 OR BSL-1.0                               |
| same-file                  | 1.0.6             | Unlicense/MIT                                       |
| scopeguard                 | 1.2.0             | MIT OR Apache-2.0                                   |
| semver                     | 1.0.28            | MIT OR Apache-2.0                                   |
| serde                      | 1.0.229           | MIT OR Apache-2.0                                   |
| serde_core                 | 1.0.229           | MIT OR Apache-2.0                                   |
| serde_derive               | 1.0.229           | MIT OR Apache-2.0                                   |
| serde_json                 | 1.0.154           | MIT OR Apache-2.0                                   |
| serde_path_to_error        | 0.1.20            | MIT OR Apache-2.0                                   |
| serde_spanned              | 0.6.9             | MIT OR Apache-2.0                                   |
| serde_yaml                 | 0.9.34+deprecated | MIT OR Apache-2.0                                   |
| shlex                      | 2.0.1             | MIT OR Apache-2.0                                   |
| simd-adler32               | 0.3.10            | MIT                                                 |
| simplecss                  | 0.2.2             | Apache-2.0 OR MIT                                   |
| siphasher                  | 1.0.4             | MIT OR Apache-2.0                                   |
| skrifa                     | 0.42.1            | MIT OR Apache-2.0                                   |
| slab                       | 0.4.12            | MIT                                                 |
| slotmap                    | 1.1.1             | Zlib                                                |
| smallvec                   | 1.16.4            | MIT OR Apache-2.0                                   |
| spin                       | 0.9.9             | MIT                                                 |
| stable_deref_trait         | 1.2.1             | MIT OR Apache-2.0                                   |
| stacker                    | 0.1.25            | MIT OR Apache-2.0                                   |
| strict-num                 | 0.1.1             | MIT                                                 |
| strum                      | 0.27.2            | MIT                                                 |
| strum_macros               | 0.27.2            | MIT                                                 |
| subsetter                  | 0.2.6             | MIT OR Apache-2.0                                   |
| svgtypes                   | 0.16.1            | Apache-2.0 OR MIT                                   |
| syn                        | 2.0.119           | MIT OR Apache-2.0                                   |
| syn                        | 3.0.9             | MIT OR Apache-2.0                                   |
| synstructure               | 0.14.0            | MIT                                                 |
| syntect                    | 5.3.0             | MIT                                                 |
| thin-vec                   | 0.2.21            | MIT OR Apache-2.0                                   |
| thiserror                  | 2.0.21            | MIT OR Apache-2.0                                   |
| thiserror-impl             | 2.0.21            | MIT OR Apache-2.0                                   |
| time                       | 0.3.55            | MIT OR Apache-2.0                                   |
| time-core                  | 0.1.9             | MIT OR Apache-2.0                                   |
| time-macros                | 0.2.32            | MIT OR Apache-2.0                                   |
| tiny-skia                  | 0.12.0            | BSD-3-Clause                                        |
| tiny-skia-path             | 0.12.0            | BSD-3-Clause                                        |
| tinystr                    | 0.8.5             | Unicode-3.0                                         |
| tinyvec                    | 1.13.3            | Zlib OR Apache-2.0 OR MIT                           |
| toml                       | 0.8.23            | MIT OR Apache-2.0                                   |
| toml_datetime              | 0.6.11            | MIT OR Apache-2.0                                   |
| toml_edit                  | 0.22.27           | MIT OR Apache-2.0                                   |
| toml_write                 | 0.1.2             | MIT OR Apache-2.0                                   |
| ttf-parser                 | 0.25.1            | MIT OR Apache-2.0                                   |
| two-face                   | 0.4.5             | MIT OR Apache-2.0                                   |
| typed-arena                | 2.0.2             | MIT                                                 |
| typst                      | 0.15.1            | Apache-2.0                                          |
| typst-assets               | 0.15.1            | Apache-2.0                                          |
| typst-eval                 | 0.15.1            | Apache-2.0                                          |
| typst-html                 | 0.15.1            | Apache-2.0                                          |
| typst-layout               | 0.15.1            | Apache-2.0                                          |
| typst-library              | 0.15.1            | Apache-2.0                                          |
| typst-macros               | 0.15.1            | Apache-2.0                                          |
| typst-pdf                  | 0.15.1            | Apache-2.0                                          |
| typst-realize              | 0.15.1            | Apache-2.0                                          |
| typst-svg                  | 0.15.1            | Apache-2.0                                          |
| typst-syntax               | 0.15.1            | Apache-2.0                                          |
| typst-timing               | 0.15.1            | Apache-2.0                                          |
| typst-utils                | 0.15.1            | Apache-2.0                                          |
| unic-langid                | 0.9.6             | MIT OR Apache-2.0                                   |
| unic-langid-impl           | 0.9.6             | MIT OR Apache-2.0                                   |
| unic-langid-macros         | 0.9.6             | MIT OR Apache-2.0                                   |
| unic-langid-macros-impl    | 0.9.6             | MIT OR Apache-2.0                                   |
| unicode-bidi               | 0.3.18            | MIT OR Apache-2.0                                   |
| unicode-bidi-mirroring     | 0.4.0             | MIT/Apache-2.0                                      |
| unicode-ccc                | 0.4.0             | MIT/Apache-2.0                                      |
| unicode-ident              | 1.0.27            | (MIT OR Apache-2.0) AND Unicode-3.0                 |
| unicode-math-class         | 0.1.0             | MIT OR Apache-2.0                                   |
| unicode-normalization      | 0.1.26            | (MIT OR Apache-2.0) AND Unicode-3.0                 |
| unicode-properties         | 0.1.4             | MIT/Apache-2.0                                      |
| unicode-script             | 0.5.8             | MIT OR Apache-2.0                                   |
| unicode-segmentation       | 1.13.3            | MIT OR Apache-2.0                                   |
| unicode-vo                 | 0.1.0             | MIT/Apache-2.0                                      |
| unsafe-libyaml             | 0.2.11            | MIT                                                 |
| unscanny                   | 0.1.0             | MIT OR Apache-2.0                                   |
| url                        | 2.5.8             | MIT OR Apache-2.0                                   |
| usvg                       | 0.47.0            | Apache-2.0 OR MIT                                   |
| utf16_iter                 | 1.0.5             | Apache-2.0 OR MIT                                   |
| utf8_iter                  | 1.0.4             | Apache-2.0 OR MIT                                   |
| vello_common               | 0.0.8             | Apache-2.0 OR MIT                                   |
| vello_common               | 0.0.9             | Apache-2.0 OR MIT                                   |
| vello_cpu                  | 0.0.8             | Apache-2.0 OR MIT                                   |
| version_check              | 0.9.5             | MIT/Apache-2.0                                      |
| walkdir                    | 2.5.0             | Unlicense/MIT                                       |
| wasmi                      | 1.1.0             | MIT/Apache-2.0                                      |
| wasmi_collections          | 1.1.0             | MIT/Apache-2.0                                      |
| wasmi_core                 | 1.1.0             | MIT/Apache-2.0                                      |
| wasmi_ir                   | 1.1.0             | MIT/Apache-2.0                                      |
| wasmparser                 | 0.239.0           | Apache-2.0 WITH LLVM-exception OR Apache-2.0 OR MIT |
| weezl                      | 0.1.12            | MIT OR Apache-2.0                                   |
| winapi-util                | 0.1.11            | not built for wasm32-wasip1                         |
| windows-link               | 0.2.1             | not built for wasm32-wasip1                         |
| windows-sys                | 0.61.2            | not built for wasm32-wasip1                         |
| winnow                     | 0.7.15            | MIT                                                 |
| write-fonts                | 0.48.1            | MIT OR Apache-2.0                                   |
| write16                    | 1.0.0             | Apache-2.0 OR MIT                                   |
| writeable                  | 0.6.4             | Unicode-3.0                                         |
| xmlwriter                  | 0.1.0             | MIT                                                 |
| xmp-writer                 | 0.3.3             | MIT OR Apache-2.0                                   |
| yaml-rust                  | 0.4.5             | MIT/Apache-2.0                                      |
| yoke                       | 0.8.4             | Unicode-3.0                                         |
| yoke-derive                | 0.8.4             | Unicode-3.0                                         |
| zerocopy                   | 0.8.63            | BSD-2-Clause OR Apache-2.0 OR MIT                   |
| zerocopy-derive            | 0.8.63            | BSD-2-Clause OR Apache-2.0 OR MIT                   |
| zerofrom                   | 0.1.8             | Unicode-3.0                                         |
| zerofrom-derive            | 0.1.8             | Unicode-3.0                                         |
| zerotrie                   | 0.2.5             | Unicode-3.0                                         |
| zerovec                    | 0.11.9            | Unicode-3.0                                         |
| zerovec-derive             | 0.11.7            | Unicode-3.0                                         |
| zlib-rs                    | 0.6.8             | Zlib                                                |
| zmij                       | 1.0.23            | MIT                                                 |
| zune-core                  | 0.5.3             | MIT OR Apache-2.0 OR Zlib                           |
| zune-jpeg                  | 0.5.15            | MIT OR Apache-2.0 OR Zlib                           |
