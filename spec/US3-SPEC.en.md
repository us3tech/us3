# U/S3 — Usenet Simplified Streaming Specification (USSS / US3)

**Version 1 — Draft, 2026-09-06** (revision 2: section 7 rewritten)

US3 defines a format for storing a single media file on Usenet as an
AES-256-encrypted, 7-Zip-compatible ZIP stream, such that it is

1. **directly streamable** (random access via HTTP range → NNTP articles), and
2. **legacy-compatible** (all articles concatenated in order = a valid ZIP
   file that opens with standard tools + password).

All access — location *and* decryption — hinges on exactly one secret:
the **US3 token**.

The key words **MUST**, **MUST NOT**, **SHOULD** are to be interpreted as
described in RFC 2119.

---

## 1. Token (Seed)

The token starts with the prefix `us3`, followed by URL-safe Base64
(RFC 4648 §5, **without padding**) of a binary blob of **55 bytes**.

Example:

```
us3AdSwMr68w8mItcl_ujBrnYD7j4N1uqf_Pks3C5-VHAW6AAAkGAEAAAAYPXxqdwFta3YMJoGPIQ
```

### 1.1 Blob layout

| Offset | Length | Field           | Type / encoding                  |
|-------:|-------:|-----------------|----------------------------------|
| 0      | 1      | version         | uint8, currently always `1`      |
| 1      | 32     | seed value      | 256 bits of cryptographic randomness |
| 33     | 8      | file size       | uint64 LE, bytes                 |
| 41     | 4      | creation time   | uint32 LE, Unix time, **unsigned** |
| 45     | 2      | block size      | uint16 LE, posting block size in KiB (×1024) |
| 47     | 3      | extension       | 3 × `a–z`/`0–9`                  |
| 50     | 1      | recovery        | uint8, PAR2 redundancy in percent |
| 51     | 4      | crc-32          | uint32 LE, CRC-32 (IEEE / PKZIP) over bytes 0–50 |

LE = little endian. Base64 length of the blob: 74 characters.

Readers MUST reject a token whose Base64 is not canonical: re-encoding the
decoded blob MUST reproduce the input string exactly. The 55 bytes occupy
440 bits while 74 Base64 characters carry 444; those four surplus bits are
not part of the blob and therefore not covered by the CRC either. Without
this check, **16 different final characters decode to the same token**, and
a typo in the last position would pass unnoticed.

### 1.2 Fields in detail

- **version** — layout version of the blob. Readers MUST reject unknown
  versions. The version feeds into every derivation via the derivation
  key `K` (section 2); v1 and v2 with an identical seed produce disjoint
  namespaces.
- **seed value** — MUST come from a CSPRNG. One seed identifies exactly
  **one** upload (see MUST rules, section 9.1).
- **file size** — size of the original media file (plaintext) in bytes.
  From this, the exact ZIP stream size and segment count are derivable
  (section 4.3); no network access and no probing is required.
- **creation time** — upload timestamp, unsigned (valid until 2106). Used
  solely for retention estimation; implementations MUST NOT depend on it
  cryptographically.
- **block size** — raw payload of one Usenet article **before** yEnc
  encoding, in KiB. Value 375 ⇒ 384,000 bytes. Valid range: **128–1000**
  (131,072–1,024,000 bytes); values outside this range MUST be rejected.
  Due to the KiB granularity, every block size is a multiple of 16
  (the AES block size) — article and AES block boundaries always coincide.
- **extension** — the extension of the original file (`mkv`, `mp4`, …):
  exactly three bytes, each from `a–z` or `0–9`; a writer stores it in
  lowercase. Determines the file name inside the ZIP (`stream.` +
  extension, section 4) and the MIME type when serving, and it becomes
  part of the path under which a reader offers the entry (section 11.3).
  Other lengths and other characters are not permitted in v1; a reader
  MUST reject a token with such an extension — a `/` or `.` in it would
  otherwise reach a file path. A writer MAY map a longer or shorter
  extension onto a three-character one naming the same container format
  (`mpeg` → `mpg`, `m2ts` → `mts`, `webm` → `mkv`); the mapped extension is
  then the one in the token and in the ZIP. Where no unambiguous equivalent
  exists, the writer refuses the file rather than guess.
- **recovery** — PAR2 redundancy in percent. `0` = no PAR2 present.
  `1–100` = redundancy share. Values > 100 are reserved and MUST be
  rejected. The percentage refers to the number of PAR2 blocks
  (section 7.1), not to bytes; since the block grid covers the whole
  stream, the recovery data amounts to the same share of `zipSize`.
  A writer holding recovery data in memory therefore needs roughly
  `recovery` percent of the file size as RAM.
- **crc-32** — protects the token against typos/transport errors
  (Base64 has no error detection of its own). Readers MUST verify the
  CRC before performing any derivation.

---

## 2. Key derivation

### 2.1 Derivation key K

```
K = Blob[0 .. 51)        # all fields EXCEPT crc-32, i.e. 51 bytes
```

Notation: `[a .. b)` is a half-open interval — byte `a` inclusive up to
byte `b` exclusive, so `[0 .. 51)` covers bytes 0–50 (length = 51).

`K` contains the version, the seed **and all parameters**. Any change to
the version, block size, file size etc. therefore automatically opens a
fresh namespace — collisions between diverging uploads are structurally
impossible, not merely forbidden.

All derivations use **HMAC-SHA-256** keyed with `K`:

```
H(ctx) = HMAC-SHA256(key = K, message = ctx)
```

### 2.2 Derivation table

| Purpose             | Context (ASCII, exact bytes)                | Result |
|---------------------|---------------------------------------------|--------|
| ZIP password        | `us3/pass`                                  | 32 bytes → base64url without padding = 43 characters |
| AES salt (WinZip)   | `us3/salt`                                  | first 16 bytes |
| Segment message-ID  | `us3/seg` ‖ LE64(i) ‖ uint8(gen)            | message-ID (section 3) |
| PAR2 message-ID     | `us3/par` ‖ LE64(j) ‖ uint8(gen)            | message-ID (section 3) |
| Article name        | `us3/name`                                  | first 8 bytes → base32 lowercase = 13 characters |

`i` = segment index within the ZIP stream (from 0), `j` = segment index
within the PAR2 stream (from 0), `gen` = generation (section 6),
initially 0.

The password is deliberately typeable (43 base64url characters) so the
legacy path — download all articles, open with 7-Zip — works without any
tooling. The weak KDF of the WinZip format (PBKDF2-HMAC-SHA1, 1000
iterations) is uncritical because the password carries 256 bits of
entropy.

The deterministic salt allows the reader to compute the AES key **without
any network access**, purely from the token (no fetch of segment 0 before
the first seek). The security condition for this is given in
section 9.1.

---

## 3. Message-ID format

From `d = H(ctx)` (32 bytes), the message-ID is formed as follows:

```
local = base32(d[0..15))   # RFC 4648 Base32, lowercase, no padding → 24 characters
label = base32(d[15..20))  # ditto → 8 characters
tld   = { 0: "net", 1: "com", 2: "org" }[ d[20] mod 3 ]

message-id = "<" local "@" label "." tld ">"
```

Example: `<tenerflxp2jcdw3sst47ehsm@qfp3gwjv.net>`

Properties:

- **No linkability:** the domain part is derived per segment as well.
  Segments of the same archive share no visible attribute.
- **Uniqueness:** 120 bits in the local part + 40 bits in the label;
  accidental collisions are practically impossible.
- **One-way function:** `K` cannot be reconstructed from published
  message-IDs.

---

## 4. ZIP container

### 4.1 Structure

The ZIP stream contains **exactly one file** named
`stream.` + extension (always 10 bytes), compression method **STORE**,
encrypted per WinZip **AE-2** (AES-256). Order within the stream:

```
[Local File Header + extra fields]          71 bytes, constant
[salt (16) | pwVerify (2)]                  18 bytes
[AES-256-CTR(plaintext)]                    fileSize bytes
[HMAC-SHA1 trailer]                         10 bytes
[Central Directory Entry]                   87 bytes, constant
[ZIP64 End of Central Directory Record]     56 bytes
[ZIP64 EOCD Locator]                        20 bytes
[End of Central Directory]                  22 bytes
```

Mandatory layout rules (only then is the stream size derivable from the
token):

- File name exactly 10 bytes (`stream.` + 3-character extension).
- The ZIP64 extra field is **always** written (20 bytes: header 4 +
  original and compressed size 8 each), even for small files; the 32-bit
  size fields are set to `0xFFFFFFFF`.
- AES extra field `0x9901` (11 bytes): vendor version 2 (AE-2), vendor
  `AE`, strength 3 (AES-256), actual method 0 (STORE). CRC-32 in the
  header = 0 (AE-2 requirement).
- Extra field order: ZIP64, then AES.
- "Compressed size" = fileSize + 28 (salt + verifier + data + trailer).
- Every header field MUST be constant across runs. In particular the DOS
  timestamp is fixed at **1980-01-01 00:00** (date `0x0021`, time `0x0000`);
  version fields, flags and attributes likewise MUST NOT vary. Writing the
  current time would make a repaired segment impossible to repost
  byte-identically, which section 9.1 rule 2 requires. Tools display the
  fixed date as the file's modification time.

### 4.2 Encryption (WinZip AE-2, quick reference)

- Key material: `PBKDF2-HMAC-SHA1(password, salt, 1000 iterations,
  66 bytes)` → 32 B AES key ‖ 32 B HMAC-SHA1 key ‖ 2 B password verifier.
- Mode: AES-CTR with a **16-byte counter, little endian, initial value 1**
  for the first block (deviating from standard CTR implementations, which
  increment big endian!).
- Random access: plaintext offset `off` ⇒ block index `b = off div 16`
  ⇒ counter value `b + 1`. Every byte is independently decryptable.
- Integrity: the AE-2 trailer (HMAC-SHA1 over the ciphertext, first
  10 bytes) protects only the whole stream and is **not** verified during
  streaming. Segment integrity during streaming is instead ensured by the
  article's yEnc CRC32; implementations SHOULD additionally verify that
  the decoded segment length matches the expected length exactly.

### 4.3 Size formula

```
zipSize   = fileSize + 284
blockLen  = blockSize × 1024
nSeg      = ⌈ zipSize / blockLen ⌉
```

Segment `i` carries the ZIP stream bytes
`[i·blockLen, min((i+1)·blockLen, zipSize))`.
Only the last segment may be shorter than `blockLen`.

Constants for offset arithmetic when reading:

```
DATA_START = 71 + 18 = 89        # ZIP stream offset of the first ciphertext byte
zipOff(v)  = 89 + v              # v = byte offset within the video (plaintext)
segIdx(v)  = zipOff(v) div blockLen
```

---

## 5. Chunking, encoding, posting

- Every segment is individually **yEnc**-encoded (with a `pcrc32` trailer)
  and posted as one article; the message-ID is computed **in advance**
  per sections 2.2/3 and set in the `Message-ID` header.
- Beyond the four bytes yEnc always escapes, encoders MUST also escape
  `.`, TAB and space at the start of a line and TAB and space at its end.
  Otherwise NNTP dot-stuffing or servers trimming trailing whitespace
  corrupt the article silently.
- **Article size is not `blockLen`.** For ciphertext-like input yEnc adds
  about **3.2 %** (4 of 256 values always escape, plus CRLF every 128
  characters), so an article measures roughly `blockLen × 1.032` plus
  about 150 bytes of markers. At the maximum `blockSize` of 1000 that is
  ~1,057,000 bytes — above the 1 MiB limit some providers enforce.
  Writers SHOULD stay at or below `blockSize` 990 unless the provider's
  limit is known to be higher. The theoretical worst case doubles the
  payload, but cannot occur with AES-CTR output.
- The file name in the yEnc header MUST be the derived article name
  (section 2.2) plus `.zip` — nondescript, but reproducible, and identical
  to the name the PAR2 set refers to (section 7.3). A downloader names the
  assembled file after it; were the two to diverge, PAR2 would describe a
  file that exists nowhere. The subject SHOULD carry that same name; the
  From header SHOULD be a **consistent pseudonym** — per-article rotating fantasy identities look like header
  falsification and conflict with common provider AUPs.
- Posting goes to exactly **one** binary newsgroup (no crossposting).
- If the server names a **different** message-ID in its `240` response, the
  poster MUST treat the upload as failed and repost under the next
  generation (section 6). Adopting the server's ID — as general-purpose
  uploaders do — would render the segment permanently unfindable, because
  the reader only ever derives IDs from the token. The two halves belong to
  different layers: the poster MUST report the failure and MUST NOT retry
  the article under the same generation — that would yield the same ID and
  the same outcome. **Reassignment under `gen+1` is the calling layer's
  responsibility**, because it presupposes state a single run does not
  have: which generation is free next follows from the upload's inventory
  (section 6), not from the POST in flight. A poster picking `gen+1` on its
  own might overwrite a generation already assigned elsewhere.
- The poster SHOULD verify every segment via `STAT` after uploading, and
  MUST do so against the **reading** server. Providers may separate posting
  and reading hosts (Eweka, for example, accepts posts only on
  `post.eweka.nl` while `news.eweka.nl` is read-only); a `STAT` against the
  posting host would confirm nothing about what the reader can later see.
  Measured propagation between the two was under four seconds.

---

## 6. Generations (recovery byte within the derivation)

Message-IDs are globally unique on Usenet; a repost under an existing ID
is rejected as long as the article is still alive on any peer. Therefore,
every ID derivation includes a **generation byte** `gen` (uint8,
initially 0):

- **Initial upload:** all segments with `gen = 0`.
- **Repair:** a lost segment `i` is reconstructed byte-identically
  (from PAR2) and reposted under `gen(i) + 1`. Only the affected segment
  changes generation; no state is stored.
- **Reader:** if `ARTICLE`/`STAT` for `(i, gen)` returns 430 (not found),
  the reader probes ascending `gen+1, gen+2, …` until success or until
  the limit `MAX_GEN` (recommended: 8, configurable). Only then is the
  segment considered lost. Discovered generations SHOULD be cached per
  session.
- Repaired segments MUST be byte-identical to the original (same
  ciphertext). Posting modified content under a new generation is
  forbidden.

The system thus remains fully stateless: token in, everything else is
derivable or discoverable.

---

## 7. PAR2 redundancy

With `recovery > 0`, the writer generates PAR2 data over the **ZIP stream**
(ciphertext; PAR2 itself is not additionally encrypted). The result is a
second byte stream, chunked with the same `blockLen` and posted under the
`us3/par` IDs. PAR2 articles follow the same generation logic as segments
(section 6).

Everything in this section is derivable from the token. The reader MUST NOT
need to fetch a PAR2 article in order to learn the shape of the PAR2 stream.

### 7.1 Block geometry

The PAR2 block size is a **multiple** of the posting block size:

```
M            = max(1, ⌈ zipSize / (4096 × blockLen) ⌉)
par2BlockLen = M × blockLen
nBlocks      = ⌈ zipSize / par2BlockLen ⌉
parityBlocks = ⌈ recovery × nBlocks / 100 ⌉
```

PAR2 block `b` covers ZIP stream bytes
`[b·par2BlockLen, min((b+1)·par2BlockLen, zipSize))`; the last block is
zero-padded to `par2BlockLen` for all hashing and coding purposes. Posting
segment `i` therefore lies in PAR2 block `⌊i / M⌋`.

Two properties follow by construction and MUST NOT be relied upon as
independent constraints:

- `nBlocks ≤ 4096`, safely below the PAR2 limit of **32,768** source blocks
  (see 7.2). Without the `M` factor, a 20 GB upload at `blockSize = 375`
  would need 52,084 blocks and no valid PAR2 set would exist.
- The coding effort, `zipSize × parityBlocks`, stays bounded. It would
  otherwise grow quadratically with the file size; see 7.6.

`par2BlockLen` is always a multiple of 1024 and thus satisfies the PAR2
requirement that the slice size be a multiple of 4.

### 7.2 Encoding

Coding is Reed-Solomon over GF(2¹⁶) as specified by PAR 2.0, little endian:

```
recovery[r] = Σ_b  const_b ^ r  ×  data_b        for r = 0 … parityBlocks−1
```

`const_b` is the `b`-th power of two whose order is 65535, i.e. the `b`-th
value `2^n` for which `n mod 3 ≠ 0`, `n mod 5 ≠ 0`, `n mod 17 ≠ 0` and
`n mod 257 ≠ 0`. Exactly 32,768 such constants exist, which is the origin of
the source block limit. The exponent `r` is written into the Recovery Slice
packet.

### 7.3 Stream layout

The PAR2 stream consists of standard PAR 2.0 packets (64-byte header:
`PAR2\0PKT` ‖ length ‖ packet MD5 ‖ recovery set ID ‖ type). It describes a
**single file**, which MUST carry the derived article name (section 2.2)
plus `.zip` — the same name the yEnc header and the legacy export use —
with length `zipSize` and slice size `par2BlockLen`.
The Creator packet MUST contain the ASCII string `us3`.

The **index set** is the packet sequence

```
Main ‖ File Description ‖ Input File Slice Checksum ‖ Creator
```

and is repeated before every group of 64 recovery slice packets, plus once
at the end:

```
[index set] [recovery 0…63] [index set] [recovery 64…127] … [index set]
```

The repetition replaces what file-based PAR2 achieves by copying the index
packets into every `volXX+YY.par2` volume: losing the first articles of the
PAR2 stream must not render the whole set unusable for legacy tools. A US3
reader does not depend on it, since it derives all parameters from the token.

One-pass note: the recovery set ID is `MD5` of the Main packet body, which
depends only on the slice size and the file ID, and the file ID is
`MD5(MD5-16k ‖ zipSize ‖ "stream.zip")`. All inputs are known after the first
16,384 bytes of the ZIP stream, so recovery slice packets can be serialised
without buffering the source.

### 7.4 Size formula

With the layout above, all packet lengths are fixed:

```
indexLen   = 360 + pad4(len(name)) + 20 × nBlocks
                                       # Main 92 + FileDesc (120 + pad4(name))
                                       #  + IFSC (80 + 20·nBlocks) + Creator 68
                                       # 17-character name → pad4 = 20 → 380 + 20·nBlocks
recvLen    = 68 + par2BlockLen
nGroups    = ⌈ parityBlocks / 64 ⌉

par2Size   = (nGroups + 1) × indexLen + parityBlocks × recvLen
nPar2Seg   = ⌈ par2Size / blockLen ⌉
```

PAR2 segment `j` carries the PAR2 stream bytes
`[j·blockLen, min((j+1)·blockLen, par2Size))`.

### 7.5 Repair

Reconstructing a lost posting segment requires the PAR2 block containing it,
and reconstructing a PAR2 block requires **all remaining source blocks** —
that is, the entire ZIP stream. This is inherent to Reed-Solomon, not a
property of this format. Implementations SHOULD collect losses from health
checks and repair them in one batch rather than one at a time.

### 7.6 Deliberate trade-off

Choosing `par2BlockLen > blockLen` coarsens repair granularity: a single lost
article consumes a whole recovery block. At `M = 3` and 12 % redundancy,
roughly 4 % of *scattered* article losses remain repairable instead of 12 %.
For *clustered* losses — whole ranges disappearing, the more common case on
Usenet — the full 12 % is retained.

The alternative, `par2BlockLen = blockLen`, keeps the finer granularity but
makes PAR2 generation the bottleneck of the upload: for the worked example in
section 10 it triples the coding effort from 2.3 TB to 6.9 TB of GF
operations, and the factor grows with file size. The constant 4096 in 7.1 is
the single point at which this trade-off is set.

Note that RAM use is *not* affected by `M`: the recovery buffer is
`recovery` percent of `zipSize` regardless.

---

## 8. Reader / streaming procedure

1. Parse the token, verify CRC, verify version, build `K`.
2. Compute password, salt → AES key locally (no network access).
3. Compute `zipSize`, `nSeg`, `DATA_START` from the token.
4. HTTP range request `[a, b]` on the video ⇒ ZIP offsets
   `[89+a, 89+b]` ⇒ segment indices ⇒ fetch articles in parallel
   (generation logic), yEnc-decode, verify CRC.
5. Assemble ciphertext bytes, compute the CTR counter from the absolute
   ciphertext offset (`zipOff − 89`), XOR, serve. MIME type from
   `extension`.
6. Sequential access patterns SHOULD be served with read-ahead
   (5–10 segments) and a segment LRU cache. The depth MUST NOT exceed the
   number of connections available: the gain is the smaller of the two, and
   anything read ahead beyond that holds the connections a real read then
   waits for. The window is refilled on every access rather than advanced
   by one — a depth of one never makes the chain longer than a single
   segment and performs like no read-ahead at all. A prefetch that fails is
   **not** a gap under §8.1: it is dropped, does not count towards the run
   of consecutive losses, and is not reported as a filled gap — nobody
   asked for those bytes.
   Note that streaming never reads the ZIP header: the reader jumps
   straight to `89 + v`. Of the 284 overhead bytes, **274 are derivable
   from the token alone** — the 89-byte head (local header, salt,
   verifier) and the 185-byte tail (central directory, ZIP64 EOCD,
   locator, EOCD). Only the 10-byte AE-2 HMAC is not, since it covers the
   entire ciphertext. A reader MAY therefore reconstruct the head of
   segment 0 or the tail of the last segment locally when those articles
   are lost and PAR2 cannot recover them; only the ciphertext portion
   must actually be fetched.
7. **Legacy export:** concatenate all segments `0 … nSeg−1` in order
   ⇒ finished ZIP file, which MUST carry the derived name; show the
   password to the user.

   The PAR2 stream MUST NOT be written out as a single file. Classic
   tools treat a bare `.par2` as an *index* file and reject one that
   carries recovery packets — gopar aborts with "recovery packets found
   in index file". The exporter therefore scans the concatenated PAR2
   segments packet by packet and distributes them:

   - `<name>.zip.par2` — one index set, no recovery packets;
   - `<name>.zip.volXX+YY.par2` — one index set plus recovery packets,
     split exponentially (1, 2, 4, 8, … blocks), where `XX` is the
     exponent of the first block in the file and `YY` the number of
     blocks. The last file may be smaller.

   No packet is rebuilt in the process, and the posted stream is
   unaffected — the split exists only on disk. An NZB MAY be
   synthesized on demand from the derived IDs but is never stored.

### 8.1 Gaps while streaming

A segment counts as **definitively lost** when the server reports it as
absent (430) in every generation `0 … MAX_GEN`, or when the article found
there is unusable (yEnc or CRC). A transport error — a dropped connection,
a timeout, or any server reply other than 430 — is **not** a loss and MUST
NOT be treated as one.

While streaming, a reader MAY serve the bytes of a definitively lost
segment as zeros instead of aborting the response in progress. It MUST log
every gap it fills, SHOULD remember the lost segment for the session
instead of repeating the generation search on every access, and MUST abort
once more than an implementation-defined number of **consecutive** segments
is missing (recommendation: 4).

For `fetch` and the legacy export (item 7) filling MUST NOT happen: it
would produce a ZIP file that looks complete and fails only on extraction,
at the AE-2 HMAC. A lost segment MUST make the export fail.

Rationale: the abort costs the user more than the gap. A player retries the
range, fails again and reads that as end of file — the film ends at the
gap. AES-CTR does not propagate errors, so the damage stays local, and
streaming never verifies the HMAC anyway (item 6). Measured in
`docs/spike-fehlstellen.md`.

### 8.2 File-system access

A reader that serves the library through a file-system interface (FUSE)
MUST report a read it cannot serve as an error and MUST NOT return fewer
bytes than requested except at the end of the file. A short read is how a
file ends: a player that receives one stops there and reports success.
Filling a definitive loss (§8.1) is not an error: the filled bytes are
served as content.

Such a reader MAY let the operating system cache the content without time
limit: a token fixes every byte of its file. The cache key MUST change when
the entry at a path is replaced by another token. That guarantee does not
hold for the zeros filled under §8.1, though, so filled content may persist
in the operating system's cache until it is evicted or the file system is
mounted again, even after a repair has made the segment available.

---

## 9. Security considerations

### 9.1 MUST rules

1. **One seed = one upload.** Under one token, different plaintext MUST
   NOT ever be encrypted — not even "the same file, remuxed again". Salt
   and password are deterministic; diverging plaintext under the same `K`
   would be CTR nonce reuse and completely breaks confidentiality. The
   ingest MUST generate a fresh seed per run, and it MUST do so **after**
   all content-modifying steps (remuxing etc.).
2. Repaired segments MUST be byte-identical (section 6).
3. The token CRC MUST be verified before any use.
4. A token whose `extension` is not exactly three bytes from `a–z` and
   `0–9` MUST be rejected (section 1.2) before the extension enters a file
   or path name.
5. A token MUST NOT appear in log messages, error texts or incidental
   output to the user — neither whole nor in a form it can be
   reconstructed from. Excepted is a command's output whose purpose is
   to hand the token to its owner. A token yields the password and full
   access (section 9.2, capability token), and a log outlives the
   session it was written in.

### 9.2 Properties and deliberate trade-offs

- **Capability token:** the token unifies location and key. Whoever holds
  the string holds the video — this makes sharing trivial (one string
  suffices) but requires treating tokens like private keys. The `us3`
  prefix additionally makes leaked tokens trivially greppable.
- **Plaintext fingerprint:** ZIP headers are unencrypted by design.
  Segment 0 of every US3 upload starts with an identical,
  machine-recognizable pattern (`PK\x03\x04`, method 99, `stream.xxx`,
  AES extra field). This does not endanger confidentiality (known
  plaintext does not break AES-CTR) but does affect inconspicuousness —
  the price of 7-Zip compatibility. The PAR2 stream carries the same
  property: its articles start with `PAR2\0PKT` packets naming the file
  `stream.zip` and the creator `us3`. Both are unavoidable consequences
  of standards compliance, and neither links the two streams to each
  other — message-IDs remain unlinkable (section 3).
- **Public availability:** all articles are retrievable by anyone.
  Confidentiality rests exclusively on the 256 bits of seed entropy.
- **No per-file forward secrecy needed:** every archive has its own seed;
  compromising one token affects exactly one video.
- **Time field:** unsigned, valid until 2106; purely informational.
- **Availability:** Usenet providers accept no liability whatsoever for
  the persistence or completeness of posted content and may remove
  content at any time. US3 with PAR2 + periodic health checks mitigates
  article loss but does not replace an independent second copy.

---

## 10. Worked example

Token:

```
us3AdSwMr68w8mItcl_ujBrnYD7j4N1uqf_Pks3C5-VHAW6AAAkGAEAAAAYPXxqdwFta3YMJoGPIQ
```

Blob (55 bytes, hex):

```
000000  01 D4 B0 32 BE BC C3 C9 88 B5 C9 7F BA 30 6B 9D
000010  80 FB 8F 83 75 BA A7 FF 3E 4B 37 0B 9F 95 1C 05
000020  BA 00 00 24 18 01 00 00 00 18 3D 7C 6A 77 01 6D
000030  6B 76 0C 26 81 8F 21
```

| Field         | Raw value                 | Meaning |
|---------------|---------------------------|---------|
| version       | `01`                      | 1 |
| seed value    | `D4B032…951C05BA`         | 256-bit randomness |
| file size     | `00 00 24 18 01 00 00 00` | 4,699,979,776 bytes |
| creation time | `18 3D 7C 6A`             | 1786527000 = 2026-08-12 09:30:00 UTC |
| block size    | `77 01`                   | 375 ⇒ 384,000 bytes |
| extension     | `6D 6B 76`                | `mkv` ⇒ file name `stream.mkv` |
| recovery      | `0C`                      | 12 % PAR2 |
| crc-32        | `26 81 8F 21` LE          | `218F8126` |

Derived values (`K` = blob bytes 0–50):

```
ZIP password : ozQ3csNcX6wgO1Mc37pkcWSRwUo75dkZVmHyoMwqQpE
AES salt     : BD5623602F144FB72A0FF89D978AE194

zipSize      : 4,699,979,776 + 284 = 4,699,980,060 bytes
nSeg         : ⌈4,699,980,060 / 384,000⌉ = 12,240 segments
```

PAR2 (section 7, `recovery` = 12):

```
M            : max(1, ⌈4,699,980,060 / (4096 × 384,000)⌉) = 3
par2BlockLen : 3 × 384,000 = 1,152,000 bytes
nBlocks      : ⌈4,699,980,060 / 1,152,000⌉ = 4,080
parityBlocks : ⌈12 × 4,080 / 100⌉ = 490
indexLen     : 380 + 20 × 4,080 = 81,980 bytes  (17-character name)
recvLen      : 68 + 1,152,000 = 1,152,068 bytes
nGroups      : ⌈490 / 64⌉ = 8
par2Size     : 9 × 81,980 + 490 × 1,152,068 = 565,251,140 bytes
nPar2Seg     : ⌈565,251,140 / 384,000⌉ = 1,473 segments

recovery buffer held by the writer : 490 × 1,152,000 = 538 MiB
```

Message-IDs:

```
Segment 0, gen 0 : <tenerflxp2jcdw3sst47ehsm@qfp3gwjv.net>
Segment 1, gen 0 : <h6gz6lanzkx5kcqcksektijr@tnxa4k6u.org>
Segment 2, gen 0 : <ejyx27oabacvm6w33fqflkqi@d2bpzdct.net>
Segment 1, gen 1 : <q2yzzia57qsfpckefqtkamzk@mkltmetf.org>   (repair repost)
PAR2 0,    gen 0 : <hdtxxuz3eel6v64zcjxexfro@qrr5y5ix.org>
```

All values are reproducible with the formulas in this specification and
serve as golden test vectors for implementations.

---

## 11. Distribution: NZBLNK

A token alone does not name its content — it carries size, block size and
three characters of extension, but no title (section 1.1). So that a
recipient knows what they are getting, US3 content is passed on as an
**NZBLNK URI** (<https://nzblnk.info/>):

```
nzblnk:?t=Mountain+Hike+2026&h=us3AQFTyRl_MyxolNxIdFOprr8od6hmu7TQq01UE91UsXQ4wMYtAAAAAAD_Xp5qdwFta3YKDNF8cg
```

The parameters follow query-string rules per RFC 3986; their order carries
no meaning. The scheme is shared with classic NZBLNK links rather than
replaced by one of our own — a recipient that serves both tells them apart
by the content of `h`.

### 11.1 Parameters

| Parameter | | Meaning in US3 |
|---|---|---|
| `h` | MUST | The complete token, including the `us3` prefix. In classic NZBLNK this holds a header fragment; in US3 the token takes its place, because every message ID follows from it (section 3). |
| `t` | MUST | Title **without** extension, in NZBLNK's sense a label. The extension comes from the token. |
| `p` | MAY | Read and discarded: the password is derived from the token (section 2.2). A sender SHOULD NOT set `p` — it would be redundant and would suggest the token is not sufficient on its own. |
| `g` | MAY | Read and discarded: where the articles live follows from the message IDs, not from a group (section 3). |

Further parameters MUST be ignored rather than rejected.

The token itself is base64url and consists only of `A-Za-z0-9-_`, so it
needs no percent encoding. Within the title, `+` or `%20` stands for a
space.

### 11.2 Recognition

A recipient recognises a US3 link by `h` starting with `us3` **and** parsing
per section 9.1, rules 3 and 4 — length, canonical base64url encoding, CRC,
version and extension. The CRC is what makes the recognition dependable: a foreign header
fragment fails either at the prefix or at the checksum.

A recipient that claims the `nzblnk:` scheme for itself SHOULD pass a link
whose `h` is not a valid US3 token on to the previously registered handler
unchanged, rather than discarding it. Only one program per system can claim
a scheme; ignoring that takes the classic links away from the user.

### 11.3 Title as file name

A sender SHOULD send `t` as a plain label — a title, not a path. Where
content ends up on the recipient's side is the recipient's decision, not
the sender's.

A recipient takes `t` **unchanged** as the file name and appends the
extension from the token. It does not tidy up: dots, hyphens and
spellings stay as they arrive, because every transformation guesses at a
point where guessing can go wrong.

```
t=Mountain.Hike.2026.4K-cam2
  ⇒   Mountain.Hike.2026.4K-cam2.mkv
```

A recipient MAY interpret a `/` in the title as separating directory
levels. That is a freedom for its own filing — for instance when the title
in its copy of the link is changed by hand to `Travel/Mountain Hike 2026/Drone
flight part 2` — not an expectation placed on the sender. If it interprets it that
way, it MUST discard `.` and `..` levels before using the title as a path.

A recipient MAY replace characters its filesystem does not carry. An empty
title, or one that is empty after this treatment, makes the link invalid.

### 11.4 Confidentiality

The link carries the token in the clear and with it complete access: where
to find the content *and* how to decrypt it (section 9). It is as
confidential as the file itself. A link therefore does not belong in search
engines, log files or public histories — and a recipient SHOULD NOT pass it
anywhere it stays permanently visible.
