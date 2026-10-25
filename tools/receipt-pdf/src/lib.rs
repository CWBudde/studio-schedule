//! Renders receipt PDFs with typst (ADR 0012).
//!
//! The crate builds to a WebAssembly reactor module (`just typst-wasm`), which
//! the server runs with wazero and the web demo with the browser's own
//! WebAssembly, so both produce the same bytes. The template and the fonts are
//! compiled in; the World reads nothing else, has no network, and its "today"
//! is fixed: every date comes from the data, so the output depends on the input
//! only.
//!
//! Input (one buffer): the length of the JSON data as a little-endian u32, the
//! JSON data (served to the template as `/data.json`), then the bytes of an
//! optional logo (served as `/logo.png` or `/logo.jpg`, whichever the template
//! asks for).
use typst::diag::{FileError, FileResult};
use typst::foundations::{Bytes, Datetime, Duration, Smart};
use typst::syntax::{FileId, RootedPath, Source, VirtualPath, VirtualRoot};
use typst::text::{Font, FontBook};
use typst::utils::LazyHash;
use typst::model::Document;
use typst::{Library, LibraryExt, World};
use typst_layout::PagedDocument;
use typst_pdf::PdfOptions;

static TEMPLATE: &str = include_str!("../receipt.typ");
static FONTS: &[&[u8]] = &[
    include_bytes!("../fonts/LibertinusSerif-Regular.otf"),
    include_bytes!("../fonts/LibertinusSerif-Bold.otf"),
    include_bytes!("../fonts/LibertinusSerif-Italic.otf"),
];

/// A typst World with the receipt template, the embedded fonts, and the data
/// and logo of the current render as its only files.
pub struct ReceiptWorld {
    library: LazyHash<Library>,
    book: LazyHash<FontBook>,
    fonts: Vec<Font>,
    main: FileId,
    data_id: FileId,
    logo_ids: [FileId; 2],
    source: Source,
    data: Bytes,
    logo: Bytes,
}

fn file_id(path: &str) -> FileId {
    RootedPath::new(VirtualRoot::Project, VirtualPath::new(path).unwrap()).intern()
}

impl Default for ReceiptWorld {
    fn default() -> Self {
        Self::new()
    }
}

impl ReceiptWorld {
    pub fn new() -> Self {
        let fonts: Vec<Font> = FONTS.iter().flat_map(|b| Font::iter(Bytes::new(*b))).collect();
        let main = file_id("/main.typ");
        Self {
            library: LazyHash::new(Library::default()),
            book: LazyHash::new(FontBook::from_fonts(&fonts)),
            fonts,
            main,
            data_id: file_id("/data.json"),
            logo_ids: [file_id("/logo.png"), file_id("/logo.jpg")],
            source: Source::new(main, TEMPLATE.to_string()),
            data: Bytes::new(Vec::new()),
            logo: Bytes::new(Vec::new()),
        }
    }

    /// Renders the template with an input buffer (see the crate docs).
    /// Errors come back as text.
    pub fn render(&mut self, input: &[u8]) -> Result<Vec<u8>, String> {
        let (data, logo) = split(input)?;
        self.data = Bytes::new(data.to_vec());
        self.logo = Bytes::new(logo.to_vec());
        let result = self.compile();
        // Drop the inputs and bound the memoization cache of a long-lived
        // instance (the typst CLI does the same).
        self.data = Bytes::new(Vec::new());
        self.logo = Bytes::new(Vec::new());
        typst::comemo::evict(10);
        result
    }

    fn compile(&self) -> Result<Vec<u8>, String> {
        let doc: PagedDocument = typst::compile(self).output.map_err(join)?;
        // The document ID is stable per receipt: the title (its number) is
        // part of the data. No timestamp, so the bytes depend on the data only.
        let ident = doc.info().title.as_ref().map(|t| t.to_string()).unwrap_or_default();
        let opts = PdfOptions {
            ident: Smart::Custom(ident),
            creator: Smart::Custom(Some("studio-schedule".into())),
            ..PdfOptions::default()
        };
        typst_pdf::pdf(&doc, &opts).map_err(join)
    }
}

/// Splits an input buffer into the JSON data and the logo.
fn split(input: &[u8]) -> Result<(&[u8], &[u8]), String> {
    if input.len() < 4 {
        return Err("input too short".into());
    }
    let n = u32::from_le_bytes([input[0], input[1], input[2], input[3]]) as usize;
    let rest = &input[4..];
    if n > rest.len() {
        return Err("data length exceeds the input".into());
    }
    Ok(rest.split_at(n))
}

fn join(errs: typst::ecow::EcoVec<typst::diag::SourceDiagnostic>) -> String {
    errs.iter().map(|e| e.message.to_string()).collect::<Vec<_>>().join("\n")
}

impl World for ReceiptWorld {
    fn library(&self) -> &LazyHash<Library> {
        &self.library
    }
    fn book(&self) -> &LazyHash<FontBook> {
        &self.book
    }
    fn main(&self) -> FileId {
        self.main
    }
    fn source(&self, id: FileId) -> FileResult<Source> {
        if id == self.main { Ok(self.source.clone()) } else { Err(FileError::AccessDenied) }
    }
    fn file(&self, id: FileId) -> FileResult<Bytes> {
        if id == self.data_id {
            Ok(self.data.clone())
        } else if self.logo_ids.contains(&id) && !self.logo.is_empty() {
            Ok(self.logo.clone())
        } else {
            Err(FileError::AccessDenied)
        }
    }
    fn font(&self, index: usize) -> Option<Font> {
        self.fonts.get(index).cloned()
    }
    // Deterministic: there is no "now"; the template takes its dates from the data.
    fn today(&self, _offset: Option<Duration>) -> Option<Datetime> {
        Datetime::from_ymd(2000, 1, 1)
    }
}

// ---- Reactor ABI (wasm): alloc(len) -> ptr; render(ptr, len) -> status;
// result_ptr()/result_len(). Status 0: the PDF is in the result buffer; 1: an
// error text. The host writes the input to the buffer alloc returned and hands
// it over to render, which frees it.
#[cfg(target_family = "wasm")]
mod abi {
    use std::cell::RefCell;
    thread_local! {
        static WORLD: RefCell<Option<super::ReceiptWorld>> = const { RefCell::new(None) };
        static RESULT: RefCell<Vec<u8>> = const { RefCell::new(Vec::new()) };
    }

    #[unsafe(no_mangle)]
    pub extern "C" fn alloc(len: usize) -> *mut u8 {
        let mut v = Vec::<u8>::with_capacity(len);
        let p = v.as_mut_ptr();
        std::mem::forget(v);
        p
    }

    /// # Safety
    /// `ptr` must come from `alloc(len)` and hold `len` initialized bytes.
    #[unsafe(no_mangle)]
    pub unsafe extern "C" fn render(ptr: *mut u8, len: usize) -> u32 {
        let input = unsafe { Vec::from_raw_parts(ptr, len, len) };
        WORLD.with(|w| {
            let mut w = w.borrow_mut();
            let world = w.get_or_insert_with(super::ReceiptWorld::new);
            let (status, out) = match world.render(&input) {
                Ok(pdf) => (0, pdf),
                Err(e) => (1, e.into_bytes()),
            };
            RESULT.with(|r| *r.borrow_mut() = out);
            status
        })
    }

    #[unsafe(no_mangle)]
    pub extern "C" fn result_ptr() -> *const u8 {
        RESULT.with(|r| r.borrow().as_ptr())
    }
    #[unsafe(no_mangle)]
    pub extern "C" fn result_len() -> usize {
        RESULT.with(|r| r.borrow().len())
    }
}
