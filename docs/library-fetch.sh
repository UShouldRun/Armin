#!/usr/bin/env bash
# Downloads armin's reference library into ~/Documents/Software: free
# and openly licensed books, official docs, standards drafts, RFCs.
# Re-running skips what is already there. Failures go to
# .armin/failed.txt.
set -u
L=${LIBRARY:-$HOME/Documents/Software}
cd "$L" || exit 1
FAILED=.armin/failed.txt
: > $FAILED
UA="Mozilla/5.0 (X11; Linux x86_64) armin-library"

log()  { echo "$(date +%H:%M:%S) $*"; }
fail() { echo "$*" >> $FAILED; log "FAILED $*"; }

# valid NAME FILE: a .pdf must really be one (not an HTML error page)
valid() {
  case "$1" in
    *.pdf) head -c 5 "$2" | grep -q '%PDF' ;;
    *) [ -s "$2" ] ;;
  esac
}

# get DIR "NAME.ext" URL: one file; PDFs are checked for %PDF
get() {
  local dir=$1 name=$2 url=$3 out="$1/$2"
  mkdir -p "$dir"
  [ -s "$out" ] && return 0
  # some hosts (SourceForge, HAL) turn curl away but serve wget
  curl -fsSL -A "$UA" --retry 3 --max-time 900 -o "$out.part" "$url" && valid "$name" "$out.part" \
    || wget -q -O "$out.part" "$url" && valid "$name" "$out.part" \
    || { rm -f "$out.part"; fail "$out <- $url"; return 1; }
  mv "$out.part" "$out"
  log "ok $out"
}

# gh DIR "NAME.ext" OWNER/REPO REGEX: newest release asset
gh() {
  local url
  [ -s "$1/$2" ] && return 0
  url=$(curl -fsSL -A "$UA" "https://api.github.com/repos/$3/releases/latest" \
    | grep -oE '"browser_download_url": *"[^"]+"' | cut -d'"' -f4 | grep -E "$4" | head -1)
  [ -n "$url" ] || { fail "$1/$2 (no release asset like $4 in $3)"; return 1; }
  get "$1" "$2" "$url"
}

# ostax DIR "NAME.pdf" SLUG: an OpenStax book
ostax() {
  local url
  [ -s "$1/$2" ] && return 0
  url=$(curl -fsSL -A "$UA" "https://openstax.org/apps/cms/api/v2/pages/?type=books.Book&slug=$3&fields=high_resolution_pdf_url" \
    | grep -oE '"high_resolution_pdf_url": *"[^"]+"' | cut -d'"' -f4)
  [ -n "$url" ] || { fail "$1/$2 (openstax $3)"; return 1; }
  get "$1" "$2" "$url"
}

# scrape DIR PAGE REGEX: every linked file on PAGE matching REGEX
scrape() {
  local dir=$1 page=$2 re=$3 base href
  base=${page%/*}
  mkdir -p "$dir"
  curl -fsSL -A "$UA" "$page" | grep -oE 'href="[^"]+"' | cut -d'"' -f2 | grep -E "$re" | sort -u |
  while read -r href; do
    case "$href" in
      http*) ;;
      /*) href="$(echo "$page" | grep -oE '^https?://[^/]+')$href" ;;
      *) href="$base/$href" ;;
    esac
    get "$dir" "$(basename "${href%%\?*}")" "$href"
  done
}

# repo DIR URL [PATHS...]: a shallow clone, only PATHS if given
repo() {
  local dir=$1 url=$2; shift 2
  [ -d "$dir/.git" ] && return 0
  mkdir -p "$(dirname "$dir")"
  if [ $# -gt 0 ]; then
    git clone -q --depth 1 --filter=blob:none --sparse "$url" "$dir" \
      && git -C "$dir" sparse-checkout set "$@" || { fail "$dir <- $url"; return 1; }
  else
    git clone -q --depth 1 "$url" "$dir" || { fail "$dir <- $url"; return 1; }
  fi
  log "ok $dir"
}

# page DIR "NAME.html" URL: one web page
page() { get "$1" "$2" "$3"; }

# --- Go -------------------------------------------------------------
D=go
page $D "The Go Programming Language Specification.html" https://go.dev/ref/spec
page $D "Effective Go.html" https://go.dev/doc/effective_go
page $D "The Go Memory Model.html" https://go.dev/ref/mem
page $D "Go FAQ.html" https://go.dev/doc/faq
page $D "Go Modules Reference.html" https://go.dev/ref/mod
page $D "A Guide to the Go Garbage Collector.html" https://go.dev/doc/gc-guide
page $D "Go Diagnostics.html" https://go.dev/doc/diagnostics
page $D "How to Write Go Code.html" https://go.dev/doc/code
page $D "Go Doc Comments.html" https://go.dev/doc/comment
page $D "Go Profile-Guided Optimization.html" https://go.dev/doc/pgo
page $D "Go Data Race Detector.html" https://go.dev/doc/articles/race_detector
repo "$D/The Little Go Book (Karl Seguin)" https://github.com/karlseguin/the-little-go-book en
repo "$D/Go 101 (Tapir Liu)" https://github.com/go101/go101 pages
repo "$D/Go by Example" https://github.com/mmcgrana/gobyexample examples
# the standard library's own docs, one text file per package
if [ ! -d "$D/stdlib" ]; then
  mkdir -p "$D/stdlib"
  for p in $(go list std | grep -vE 'internal|vendor|^cmd/'); do
    go doc -all "$p" > "$D/stdlib/$(echo "$p" | tr / .).txt" 2> /dev/null
  done
  log "ok $D/stdlib ($(ls $D/stdlib | wc -l) packages)"
fi

# --- C --------------------------------------------------------------
D=c
get $D "C11 Standard Draft N1570.pdf" https://www.open-std.org/jtc1/sc22/wg14/www/docs/n1570.pdf
get $D "C23 Standard Draft N3220.pdf" https://www.open-std.org/jtc1/sc22/wg14/www/docs/n3220.pdf
get $D "The GNU C Library Reference Manual.pdf" https://sourceware.org/glibc/manual/latest/pdf/libc.pdf
get $D "Using the GNU Compiler Collection (GCC).pdf" https://gcc.gnu.org/onlinedocs/gcc.pdf
get $D "Modern C (Jens Gustedt).pdf" https://inria.hal.science/hal-02383654/document
# Linux man pages: system calls (2), C library (3), overviews (7)
for s in 2 3 7; do
  if [ ! -d "$D/man$s" ]; then
    mkdir -p "$D/man$s"
    for f in /usr/share/man/man$s/*; do
      n=$(basename "$f" .gz)
      MANWIDTH=80 man -l "$f" 2> /dev/null | col -b > "$D/man$s/$n.txt"
    done
    log "ok $D/man$s"
  fi
done

# --- Beej's guides --------------------------------------------------
D=beej
for g in "bgc:Beej's Guide to C Programming" "bgclr:Beej's Guide to the C Standard Library" \
  "bgnet:Beej's Guide to Network Programming" "bgnet0:Beej's Guide to Network Concepts" \
  "bgipc:Beej's Guide to Unix IPC" "bggit:Beej's Guide to Git" \
  "bglcs:Beej's Guide to Learning Computer Science" "bgpython:Beej's Guide to Python Programming"; do
  get $D "${g#*:}.pdf" "https://beej.us/guide/${g%%:*}/pdf/${g%%:*}_a4_c_1.pdf"
done
page $D "Beej's Quick Guide to GDB.html" https://beej.us/guide/bggdb/

# --- Linux and the command line ---------------------------------------
D=linux
get $D "The Linux Command Line (William Shotts).pdf" https://sourceforge.net/projects/linuxcommand/files/TLCL/25.12/TLCL-25.12A.pdf/download
get $D "Adventures with the Linux Command Line (William Shotts).pdf" https://sourceforge.net/projects/linuxcommand/files/AWTLCL/21.10/AWTLCL-21.10.pdf/download
get $D "Advanced Bash-Scripting Guide (Mendel Cooper).pdf" https://tldp.org/LDP/abs/abs-guide.pdf
get $D "GNU Bash Reference Manual.pdf" https://www.gnu.org/software/bash/manual/bash.pdf
get $D "GNU Coreutils Manual.pdf" https://www.gnu.org/software/coreutils/manual/coreutils.pdf
get $D "GNU Make Manual.pdf" https://www.gnu.org/software/make/manual/make.pdf
scrape "$D/Linux Device Drivers 3rd Edition (Corbet, Rubini, Kroah-Hartman)" https://lwn.net/Kernel/LDD3/ '\.pdf$'
# commands (section 1) and admin (section 8)
for s in 1 8; do
  if [ ! -d "$D/man$s" ]; then
    mkdir -p "$D/man$s"
    for f in /usr/share/man/man$s/*; do
      n=$(basename "$f" .gz)
      MANWIDTH=80 man -l "$f" 2> /dev/null | col -b > "$D/man$s/$n.txt"
    done
    log "ok $D/man$s"
  fi
done

# --- Git --------------------------------------------------------------
D=git
gh $D "Pro Git (Chacon, Straub).pdf" progit/progit2 'progit\.pdf$'
get $D "Git Magic (Ben Lynn).pdf" http://www-cs-students.stanford.edu/~blynn/gitmagic/book.pdf

# --- Operating systems ------------------------------------------------
D=os
scrape "$D/Operating Systems - Three Easy Pieces (Arpaci-Dusseau)" https://pages.cs.wisc.edu/~remzi/OSTEP/ '\.pdf$'
get $D "xv6 - a simple Unix-like teaching operating system (Cox, Kaashoek, Morris).pdf" https://pdos.csail.mit.edu/6.1810/2024/xv6/book-riscv-rev4.pdf
get $D "The Little Book of Semaphores (Allen Downey).pdf" https://greenteapress.com/semaphores/LittleBookOfSemaphores.pdf
get $D "Think OS (Allen Downey).pdf" https://greenteapress.com/thinkos/thinkos.pdf
get $D "Operating Systems From 0 to 1 (Tu Do).pdf" https://github.com/tuhdo/os01/raw/master/Operating_Systems_From_0_to_1.pdf

# --- Data structures and algorithms --------------------------------------
D=algorithms
get $D "Open Data Structures in C++ (Pat Morin).pdf" https://opendatastructures.org/ods-cpp.pdf
get $D "Algorithms (Jeff Erickson).pdf" https://jeffe.cs.illinois.edu/teaching/algorithms/book/Algorithms-JeffE.pdf
get $D "Competitive Programmer's Handbook (Antti Laaksonen).pdf" https://cses.fi/book/book.pdf
get $D "Problems on Algorithms (Ian Parberry).pdf" https://ianparberry.com/books/free/poa.pdf

# --- Performance and memory ---------------------------------------------
D=performance
get $D "What Every Programmer Should Know About Memory (Ulrich Drepper).pdf" https://people.freebsd.org/~lstewart/articles/cpumemory.pdf
scrape $D https://mirrors.edge.kernel.org/pub/linux/kernel/people/paulmck/perfbook/perfbook.html 'perfbook\.[0-9.a-z]*\.pdf$'
for m in optimizing_cpp:"Optimizing Software in C++" optimizing_assembly:"Optimizing Subroutines in Assembly" \
  microarchitecture:"The Microarchitecture of Intel, AMD and VIA CPUs" instruction_tables:"Instruction Tables" \
  calling_conventions:"Calling Conventions"; do
  get $D "${m#*:} (Agner Fog).pdf" "https://www.agner.org/optimize/${m%%:*}.pdf"
done
repo "$D/Algorithms for Modern Hardware (Sergey Slotin)" https://github.com/algorithmica-org/algorithmica content/english/hpc

# --- Hardware, architecture, assembly -----------------------------------
D=hardware
get $D "Programming from the Ground Up (Jonathan Bartlett).pdf" https://download-mirror.savannah.gnu.org/releases/pgubook/ProgrammingGroundUp-1-0-booksize.pdf
get $D "PC Assembly Language (Paul Carter).pdf" https://pacman128.github.io/static/pcasm-book.pdf
gh $D "The RISC-V Instruction Set Manual.pdf" riscv/riscv-isa-manual 'riscv-spec\.pdf$'
get $D "Intel 64 and IA-32 Architectures Software Developer's Manual.pdf" https://cdrdv2.intel.com/v1/dl/getContent/671200
get $D "Lessons in Electric Circuits Vol IV Digital (Tony Kuphaldt).pdf" https://www.ibiblio.org/kuphaldt/electricCircuits/Digital/DIGI.pdf
get $D "Lessons in Electric Circuits Vol I DC (Tony Kuphaldt).pdf" https://www.ibiblio.org/kuphaldt/electricCircuits/DC/DC.pdf
if [ ! -d "$D/Dive into Systems (Matthews, Newhall, Webb)" ]; then
  # an HTML book: its chapter pages, no images
  wget -q -r -l 1 -np -nd -A html -P "$D/Dive into Systems (Matthews, Newhall, Webb)" https://diveintosystems.org/book/ \
    && log "ok $D/Dive into Systems" || fail "$D/Dive into Systems"
fi

# --- Embedded systems -------------------------------------------------
D=embedded
get $D "Introduction to Embedded Systems (Lee, Seshia).pdf" https://ptolemy.berkeley.edu/books/leeseshia/releases/LeeSeshia_DigitalV2_2.pdf
gh $D "Mastering the FreeRTOS Real Time Kernel.pdf" FreeRTOS/FreeRTOS-Kernel-Book '\.pdf$'
repo "$D/Bare Metal Programming Guide (Sergey Lyubka)" https://github.com/cpq/bare-metal-programming-guide

# --- Networking -------------------------------------------------------
D=networking
repo "$D/Computer Networks - A Systems Approach (Peterson, Davie)" https://github.com/SystemsApproach/book
for r in 9110:"HTTP Semantics" 9112:"HTTP 1.1" 9113:"HTTP 2" 9114:"HTTP 3" 9293:"TCP" \
  768:"UDP" 791:"IPv4" 8200:"IPv6" 8446:"TLS 1.3" 6455:"WebSocket" 1034:"DNS Concepts" \
  1035:"DNS Implementation" 3986:"URI Syntax" 8259:"JSON" 9000:"QUIC" 5321:"SMTP"; do
  get "$D/rfc" "RFC ${r%%:*} - ${r#*:}.txt" "https://www.rfc-editor.org/rfc/rfc${r%%:*}.txt"
done

# --- Backend and distributed systems ------------------------------------
D=backend
page $D "Distributed Systems for Fun and Profit (Mikito Takada).html" http://book.mixu.net/distsys/ebook.html
repo "$D/The System Design Primer (Donne Martin)" https://github.com/donnemartin/system-design-primer
repo "$D/Build Web Application with Golang (astaxie)" https://github.com/astaxie/build-web-application-with-golang en
repo "$D/The Twelve-Factor App" https://github.com/heroku/12factor content/en

# --- Databases --------------------------------------------------------
D=database
get $D "Architecture of a Database System (Hellerstein, Stonebraker, Hamilton).pdf" https://dsf.berkeley.edu/papers/fntdb07-architecture.pdf
get $D "Readings in Database Systems 5th Edition (Red Book).pdf" http://www.redbook.io/pdf/redbook-5th-edition.pdf
get $D "PostgreSQL 17 Documentation.pdf" https://www.postgresql.org/files/documentation/pdf/17/postgresql-17-A4.pdf
if [ ! -d "$D/SQLite Documentation" ]; then
  z=$(curl -fsSL -A "$UA" https://www.sqlite.org/download.html | grep -oE "'[0-9]{4}/sqlite-doc-[0-9]+\.zip'" | head -1 | tr -d "'")
  if [ -n "$z" ] && curl -fsSL -A "$UA" -o /tmp/sqlite-doc.zip "https://www.sqlite.org/$z"; then
    mkdir -p "$D/SQLite Documentation" && unzip -q -o /tmp/sqlite-doc.zip -d "$D/SQLite Documentation" && rm /tmp/sqlite-doc.zip
    log "ok $D/SQLite Documentation"
  else
    fail "$D/SQLite Documentation"
  fi
fi

# --- Frontend and the web ---------------------------------------------
D=frontend
get $D "Eloquent JavaScript (Marijn Haverbeke).pdf" https://eloquentjavascript.net/Eloquent_JavaScript.pdf
repo "$D/You Don't Know JS Yet (Kyle Simpson)" https://github.com/getify/You-Dont-Know-JS
repo "$D/MDN Web Docs" https://github.com/mdn/content files/en-us/web/html files/en-us/web/css \
  files/en-us/web/javascript files/en-us/web/http files/en-us/web/api files/en-us/web/accessibility

# --- Security and cryptography ------------------------------------------
D=security
scrape "$D/Security Engineering 2nd Edition (Ross Anderson)" https://www.cl.cam.ac.uk/~rja14/book.html '^SEv2-.*\.pdf$'
get $D "A Graduate Course in Applied Cryptography (Boneh, Shoup).pdf" https://toc.cryptobook.us/book.pdf
get $D "Crypto 101 (Laurens Van Houtven).pdf" https://www.crypto101.io/Crypto101.pdf
gh $D "OWASP Web Security Testing Guide.pdf" OWASP/wstg '\.pdf$'
repo "$D/OWASP Cheat Sheet Series" https://github.com/OWASP/CheatSheetSeries cheatsheets

# --- Compilers and languages -----------------------------------------
D=compilers
repo "$D/Crafting Interpreters (Robert Nystrom)" https://github.com/munificent/craftinginterpreters book
get $D "Compiler Construction (Niklaus Wirth).pdf" https://people.inf.ethz.ch/wirth/CompilerConstruction/CompilerConstruction1.pdf
get $D "Compiler Construction Part 2 (Niklaus Wirth).pdf" https://people.inf.ethz.ch/wirth/CompilerConstruction/CompilerConstruction2.pdf

# --- Mathematics ------------------------------------------------------
D=math
get $D "Calculus (Gilbert Strang).pdf" https://ocw.mit.edu/courses/res-18-001-calculus-fall-2023/mitres_18_001_f17_full_book.pdf
ostax $D "Calculus Volume 1 (OpenStax).pdf" calculus-volume-1
ostax $D "Calculus Volume 2 (OpenStax).pdf" calculus-volume-2
ostax $D "Calculus Volume 3 (OpenStax).pdf" calculus-volume-3
get $D "Linear Algebra Done Right 4th Edition (Sheldon Axler).pdf" https://linear.axler.net/LADR4e.pdf
get $D "Linear Algebra (Jim Hefferon).pdf" https://jheffero.w3.uvm.edu/linearalgebra/book.pdf
get $D "Mathematics for Computer Science (Lehman, Leighton, Meyer).pdf" https://courses.csail.mit.edu/6.042/spring18/mcs.pdf
get $D "Discrete Mathematics - An Open Introduction (Oscar Levin).pdf" https://discrete.openmathbooks.org/pdfs/dmoi4.pdf
get $D "Book of Proof (Richard Hammack).pdf" https://richardhammack.github.io/BookOfProof/Main.pdf
get $D "forall x Calgary - An Introduction to Formal Logic.pdf" https://forallx.openlogicproject.org/forallxyyc.pdf
get $D "Sets, Logic, Computation (Open Logic Project).pdf" https://slc.openlogicproject.org/slc-screen.pdf
get $D "Introduction to Probability (Grinstead, Snell).pdf" https://math.dartmouth.edu/~prob/prob/prob.pdf
get $D "Think Stats 2nd Edition (Allen Downey).pdf" https://greenteapress.com/thinkstats2/thinkstats2.pdf
get $D "Think Bayes (Allen Downey).pdf" https://greenteapress.com/thinkbayes/thinkbayes.pdf
get $D "Abstract Algebra - Theory and Applications (Thomas Judson).pdf" https://judsonbooks.org/aata-files/aata-20260811.pdf
get $D "Introduction to Theory of Computation (Maheshwari, Smid).pdf" https://cglab.ca/~michiel/TheoryOfComputation/TheoryOfComputation.pdf
get $D "Information Theory, Inference, and Learning Algorithms (David MacKay).pdf" https://www.inference.org.uk/itprnn/book.pdf
get $D "Numerical Algorithms (Justin Solomon).pdf" https://people.csail.mit.edu/jsolomon/share/book/numerical_book.pdf

# --- Physics ----------------------------------------------------------
D=physics
ostax $D "University Physics Volume 1 - Mechanics (OpenStax).pdf" university-physics-volume-1
ostax $D "University Physics Volume 2 - Thermodynamics, Electricity, Magnetism (OpenStax).pdf" university-physics-volume-2
ostax $D "University Physics Volume 3 - Optics, Modern Physics (OpenStax).pdf" university-physics-volume-3
get $D "Light and Matter (Benjamin Crowell).pdf" https://archive.org/download/lm_20220102/lm.pdf
get $D "Classical Dynamics (David Tong).pdf" https://www.davidtong.org/pdfs/teaching/dynamics/clas.pdf
get $D "Lectures on Classical Mechanics (John Baez).pdf" https://math.ucr.edu/home/baez/classical/texfiles/2005/book/classical.pdf
get $D "Electromagnetism (David Tong).pdf" https://www.davidtong.org/pdfs/teaching/electromagnetism/electro.pdf
repo "$D/The Nature of Code (Daniel Shiffman)" https://github.com/nature-of-code/noc-book-2 content

# --- Other sciences close to code -----------------------------------------
D=science
get $D "Pattern Recognition and Machine Learning (Christopher Bishop).pdf" https://www.microsoft.com/en-us/research/uploads/prod/2006/01/Bishop-Pattern-Recognition-and-Machine-Learning-2006.pdf
get $D "Mathematics for Machine Learning (Deisenroth, Faisal, Ong).pdf" https://mml-book.github.io/book/mml-book.pdf
gh $D "Understanding Deep Learning (Simon Prince).pdf" udlbook/udlbook 'UnderstandingDeepLearning.*\.pdf$'
get $D "Dive into Deep Learning (Zhang, Lipton, Li, Smola).pdf" https://d2l.ai/d2l-en.pdf
get $D "Think DSP (Allen Downey).pdf" https://greenteapress.com/thinkdsp/thinkdsp.pdf
scrape "$D/The Scientist and Engineer's Guide to Digital Signal Processing (Steven Smith)" https://www.dspguide.com/pdfbook.htm '\.pdf$'
get $D "Learn OpenGL (Joey de Vries).pdf" https://learnopengl.com/book/book_pdf.pdf
repo "$D/Ray Tracing in One Weekend series (Peter Shirley)" https://github.com/RayTracing/raytracing.github.io books

log "done; $(wc -l < $FAILED) failed (see $FAILED)"
