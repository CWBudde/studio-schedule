//! Development preview: renders the receipt template natively.
//!
//!     cargo run --release -- data.json [logo.png] > receipt.pdf
use std::io::Write;

fn main() {
    let args: Vec<String> = std::env::args().skip(1).collect();
    if args.is_empty() || args.len() > 2 {
        eprintln!("usage: receipt-pdf data.json [logo] > receipt.pdf");
        std::process::exit(2);
    }
    let data = std::fs::read(&args[0]).expect("read data");
    let logo = args.get(1).map(|p| std::fs::read(p).expect("read logo")).unwrap_or_default();
    let mut input = (data.len() as u32).to_le_bytes().to_vec();
    input.extend_from_slice(&data);
    input.extend_from_slice(&logo);
    match receipt_pdf::ReceiptWorld::new().render(&input) {
        Ok(pdf) => std::io::stdout().write_all(&pdf).expect("write stdout"),
        Err(e) => {
            eprintln!("error: {e}");
            std::process::exit(1);
        }
    }
}
