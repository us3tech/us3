# U/S3 — Usenet Simplified Streaming Specification (USSS / US3)

**Version 1 — Entwurf, Stand 2026-09-06** (Revision 2: Abschnitt 7 neu gefasst)

US3 definiert ein Format, um eine einzelne Mediendatei als AES-256-verschlüsselten,
7-Zip-kompatiblen ZIP-Strom im Usenet abzulegen, sodass sie

1. **direkt streambar** ist (wahlfreier Zugriff über HTTP-Range → NNTP-Artikel), und
2. **legacy-kompatibel** bleibt (alle Artikel in Reihenfolge = gültige, mit
   Standard-Tools + Passwort öffenbare ZIP-Datei).

Der gesamte Zugriff — Fundort *und* Entschlüsselung — hängt an genau einem
Geheimnis: dem **US3-Token**.

Schlüsselwörter **MUSS**, **DARF NICHT**, **SOLLTE** sind im Sinne von RFC 2119
zu verstehen.

---

## 1. Token (Seed)

Das Token beginnt mit dem Präfix `us3`, gefolgt von URL-sicherem Base64
(RFC 4648 §5, **ohne Padding**) eines binären Blobs von **55 Bytes**.

Beispiel:

```
us3AdSwMr68w8mItcl_ujBrnYD7j4N1uqf_Pks3C5-VHAW6AAAkGAEAAAAYPXxqdwFta3YMJoGPIQ
```

### 1.1 Blob-Layout

| Offset | Länge | Feld            | Typ / Kodierung                  |
|-------:|------:|-----------------|----------------------------------|
| 0      | 1     | version         | uint8, aktuell immer `1`         |
| 1      | 32    | seed value      | 256 Bit kryptographischer Zufall |
| 33     | 8     | file size       | uint64 LE, Bytes                 |
| 41     | 4     | creation time   | uint32 LE, Unix-Zeit, **unsigned** |
| 45     | 2     | block size      | uint16 LE, Posting-Blockgröße in KiB (×1024) |
| 47     | 3     | extension       | 3 × `a–z`/`0–9`                  |
| 50     | 1     | recovery        | uint8, PAR2-Redundanz in Prozent |
| 51     | 4     | crc-32          | uint32 LE, CRC-32 (IEEE / PKZIP) über Bytes 0–50 |

LE = Little Endian. Base64-Länge des Blobs: 74 Zeichen.

Reader MÜSSEN ein Token ablehnen, dessen Base64 nicht kanonisch ist: Das
erneute Kodieren des dekodierten Blobs MUSS die Eingabe exakt wiedergeben.
Die 55 Bytes belegen 440 Bit, 74 Base64-Zeichen tragen 444; diese vier
überzähligen Bit gehören nicht zum Blob und liegen damit auch nicht unter
der CRC. Ohne diese Prüfung dekodieren **16 verschiedene Endzeichen zum
selben Token**, und ein Tippfehler an letzter Stelle bliebe unbemerkt.

### 1.2 Felder im Detail

- **version** — Layout-Version des Blobs. Unbekannte Versionen MÜSSEN vom
  Reader abgelehnt werden. Die Version fließt über den Ableitungsschlüssel `K`
  (Abschnitt 2) automatisch in alle Ableitungen ein; v1 und v2 mit identischem
  Seed erzeugen disjunkte Namensräume.
- **seed value** — MUSS aus einem CSPRNG stammen. Ein Seed identifiziert genau
  **einen** Upload (siehe MUSS-Regeln, Abschnitt 9.1).
- **file size** — Größe der Original-Mediendatei (Plaintext) in Bytes. Daraus
  ist die exakte ZIP-Stromgröße und Segmentanzahl ableitbar (Abschnitt 4.3);
  es ist kein Netzwerkzugriff und kein Probing nötig.
- **creation time** — Upload-Zeitpunkt, unsigned (gültig bis 2106). Dient
  ausschließlich der Retention-Abschätzung; Implementierungen DÜRFEN NICHT
  kryptographisch davon abhängen.
- **block size** — Roh-Payload eines Usenet-Artikels **vor** yEnc-Kodierung,
  in KiB. Wert 375 ⇒ 384.000 Bytes. Gültiger Bereich: **128–1000**
  (131.072–1.024.000 Bytes); Werte außerhalb MÜSSEN abgelehnt werden.
  Durch die KiB-Granularität ist jede Blockgröße ein Vielfaches von 16
  (AES-Blockgröße) — Artikel- und AES-Blockgrenzen fallen stets zusammen.
- **extension** — die Endung der Originaldatei (`mkv`, `mp4`, …): genau drei
  Bytes, jedes aus `a–z` oder `0–9`; ein Writer schreibt sie klein. Bestimmt
  den Dateinamen im ZIP (`stream.` + extension, Abschnitt 4) und den
  MIME-Type beim Serving, und sie wird Teil des Pfads, unter dem ein Reader
  den Eintrag anbietet (Abschnitt 11.3). Andere Längen und andere Zeichen
  sind in v1 nicht zulässig; ein Reader MUSS ein Token mit einer solchen
  Endung ablehnen — ein `/` oder `.` darin würde sonst in einen Dateipfad
  gelangen. Ein Writer DARF eine längere oder kürzere Endung auf eine
  dreistellige abbilden, die dasselbe Containerformat bezeichnet (`mpeg` →
  `mpg`, `m2ts` → `mts`, `webm` → `mkv`); im Token und im ZIP steht dann die
  abgebildete Endung. Gibt es keine eindeutige Entsprechung, lehnt der
  Writer die Datei ab, statt zu raten.
- **recovery** — PAR2-Redundanz in Prozent. `0` = kein PAR2 vorhanden.
  `1–100` = Redundanzanteil. Werte > 100 sind reserviert und MÜSSEN
  abgelehnt werden. Der Prozentsatz bezieht sich auf die Anzahl der
  PAR2-Blöcke (Abschnitt 7.1), nicht auf Bytes; da das Blockraster den
  gesamten Strom abdeckt, entspricht die Recovery-Datenmenge demselben
  Anteil von `zipSize`. Ein Writer, der die Recovery-Daten im Speicher
  hält, braucht daher rund `recovery` Prozent der Dateigröße als RAM.
- **crc-32** — schützt das Token gegen Tippfehler/Transportfehler
  (Base64 hat keine eigene Fehlererkennung). Reader MÜSSEN den CRC prüfen,
  bevor irgendeine Ableitung erfolgt.

---

## 2. Schlüsselableitung

### 2.1 Ableitungsschlüssel K

```
K = Blob[0 .. 51)        # alle Felder AUSSER crc-32, also 51 Bytes
```

`K` enthält Version, Seed **und alle Parameter**. Jede Änderung an Version,
Blockgröße, Dateigröße etc. öffnet damit automatisch einen frischen
Namensraum — Kollisionen zwischen abweichenden Uploads sind strukturell
ausgeschlossen, nicht nur verboten.

Alle Ableitungen verwenden **HMAC-SHA-256** mit Schlüssel `K`:

```
H(ctx) = HMAC-SHA256(key = K, message = ctx)
```

### 2.2 Ableitungstabelle

| Zweck               | Kontext (ASCII, exakte Bytes)               | Ergebnis |
|---------------------|---------------------------------------------|----------|
| ZIP-Passwort        | `us3/pass`                                  | 32 Bytes → base64url ohne Padding = 43 Zeichen |
| AES-Salt (WinZip)   | `us3/salt`                                  | erste 16 Bytes |
| Segment-Message-ID  | `us3/seg` ‖ LE64(i) ‖ uint8(gen)            | Message-ID (Abschnitt 3) |
| PAR2-Message-ID     | `us3/par` ‖ LE64(j) ‖ uint8(gen)            | Message-ID (Abschnitt 3) |
| Artikelname         | `us3/name`                                  | erste 8 Bytes → base32 kleingeschrieben = 13 Zeichen |

`i` = Segmentindex im ZIP-Strom (ab 0), `j` = Segmentindex im PAR2-Strom
(ab 0), `gen` = Generation (Abschnitt 6), initial 0.

Das Passwort ist bewusst tippbar (43 base64url-Zeichen), damit der
Legacy-Pfad — Download aller Artikel, Öffnen mit 7-Zip — ohne Tooling
funktioniert. Die schwache KDF des WinZip-Formats (PBKDF2-HMAC-SHA1,
1000 Iterationen) ist unkritisch, weil das Passwort 256 Bit Entropie trägt.

Der deterministische Salt erlaubt es dem Reader, den AES-Schlüssel **ohne
Netzwerkzugriff** allein aus dem Token zu berechnen (kein Fetch von
Segment 0 vor dem ersten Seek nötig). Die Sicherheitsbedingung dafür steht
in Abschnitt 9.1.

---

## 3. Message-ID-Format

Aus `d = H(ctx)` (32 Bytes) wird die Message-ID wie folgt gebildet:

```
local = base32(d[0..15))   # RFC-4648-Base32, kleingeschrieben, ohne Padding → 24 Zeichen
label = base32(d[15..20))  # dito → 8 Zeichen
tld   = { 0: "net", 1: "com", 2: "org" }[ d[20] mod 3 ]

message-id = "<" local "@" label "." tld ">"
```

Beispiel: `<tenerflxp2jcdw3sst47ehsm@qfp3gwjv.net>`

Eigenschaften:

- **Keine Verkettbarkeit:** Auch der Domain-Teil ist pro Segment abgeleitet.
  Segmente desselben Archivs teilen kein sichtbares Merkmal.
- **Eindeutigkeit:** 120 Bit im Local-Part + 40 Bit im Label; zufällige
  Kollisionen sind praktisch ausgeschlossen.
- **Einwegfunktion:** Aus veröffentlichten Message-IDs ist `K` nicht
  rekonstruierbar.

---

## 4. ZIP-Container

### 4.1 Aufbau

Der ZIP-Strom enthält **genau eine Datei** mit dem Namen
`stream.` + extension (immer 10 Bytes), Kompressionsmethode **STORE**,
verschlüsselt nach WinZip **AE-2** (AES-256). Reihenfolge im Strom:

```
[Local File Header + Extrafelder]           71 Bytes, konstant
[salt (16) | pwVerify (2)]                  18 Bytes
[AES-256-CTR(plaintext)]                    fileSize Bytes
[HMAC-SHA1-Trailer]                         10 Bytes
[Central Directory Entry]                   87 Bytes, konstant
[ZIP64 End of Central Directory Record]     56 Bytes
[ZIP64 EOCD Locator]                        20 Bytes
[End of Central Directory]                  22 Bytes
```

Verbindliche Layout-Regeln (nur so ist die Stromgröße aus dem Token
ableitbar):

- Dateiname exakt 10 Bytes (`stream.` + 3 Zeichen Endung).
- ZIP64-Extrafeld wird **immer** geschrieben (20 Bytes: Header 4 +
  Original- und Komprimiertgröße je 8), auch bei kleinen Dateien;
  die 32-Bit-Größenfelder stehen auf `0xFFFFFFFF`.
- AES-Extrafeld `0x9901` (11 Bytes): Vendor-Version 2 (AE-2), Vendor `AE`,
  Strength 3 (AES-256), echte Methode 0 (STORE). CRC-32 im Header = 0
  (AE-2-Vorgabe).
- Extrafeld-Reihenfolge: ZIP64, dann AES.
- „Compressed size“ = fileSize + 28 (Salt + Verifier + Daten + Trailer).
- Jedes Header-Feld MUSS über alle Läufe konstant sein. Insbesondere steht
  der DOS-Zeitstempel fest auf **1980-01-01 00:00** (Datum `0x0021`, Zeit
  `0x0000`); ebenso DÜRFEN Versionsfelder, Flags und Attribute NICHT
  variieren. Die aktuelle Zeit einzutragen machte es unmöglich, ein
  repariertes Segment bytegleich neu zu posten, wie es Abschnitt 9.1,
  Regel 2 verlangt. Werkzeuge zeigen das feste Datum als Änderungszeit an.

### 4.2 Verschlüsselung (WinZip AE-2, Kurzreferenz)

- Schlüsselmaterial: `PBKDF2-HMAC-SHA1(password, salt, 1000 Iterationen,
  66 Bytes)` → 32 B AES-Key ‖ 32 B HMAC-SHA1-Key ‖ 2 B Passwort-Verifier.
- Modus: AES-CTR mit **16-Byte-Zähler, Little Endian, Startwert 1** für den
  ersten Block (abweichend von Standard-CTR-Implementierungen, die
  Big Endian inkrementieren!).
- Wahlfreier Zugriff: Plaintext-Offset `off` ⇒ Blockindex `b = off div 16`
  ⇒ Zählerwert `b + 1`. Jedes Byte ist unabhängig entschlüsselbar.
- Integrität: Der AE-2-Trailer (HMAC-SHA1 über den Ciphertext, erste
  10 Bytes) sichert nur den Gesamtstrom und wird beim Streaming **nicht**
  geprüft. Segmentintegrität beim Streaming sichert stattdessen der
  yEnc-CRC32 des Artikels; Implementierungen SOLLTEN zusätzlich verifizieren,
  dass die dekodierte Segmentlänge exakt der erwarteten entspricht.

### 4.3 Größenformel

```
zipSize   = fileSize + 284
blockLen  = blockSize × 1024
nSeg      = ⌈ zipSize / blockLen ⌉
```

Segment `i` trägt die ZIP-Strom-Bytes `[i·blockLen, min((i+1)·blockLen, zipSize))`.
Nur das letzte Segment darf kürzer als `blockLen` sein.

Konstanten für die Offset-Arithmetik beim Lesen:

```
DATA_START = 71 + 18 = 89        # ZIP-Strom-Offset des ersten Ciphertext-Bytes
zipOff(v)  = 89 + v              # v = Byte-Offset im Video (Plaintext)
segIdx(v)  = zipOff(v) div blockLen
```

---

## 5. Chunking, Kodierung, Posting

- Jedes Segment wird einzeln **yEnc**-kodiert (mit `pcrc32`-Trailer) und als
  ein Artikel gepostet; die Message-ID wird gemäß Abschnitt 2.2/3 **vorab
  berechnet** und im `Message-ID`-Header gesetzt.
- Über die vier stets maskierten Bytes hinaus MÜSSEN Kodierer auch `.`,
  TAB und Leerzeichen am Zeilenanfang sowie TAB und Leerzeichen am
  Zeilenende maskieren. Sonst zerstören das NNTP-Dot-Stuffing oder Server,
  die abschließenden Leerraum kürzen, den Artikel unbemerkt.
- **Die Artikelgröße ist nicht `blockLen`.** Bei ciphertext-artigem Inhalt
  schlägt yEnc rund **3,2 %** auf (4 von 256 Werten werden immer maskiert,
  dazu CRLF alle 128 Zeichen). Ein Artikel misst damit etwa
  `blockLen × 1,032` plus rund 150 Bytes Marken. Bei der Obergrenze
  `blockSize` = 1000 sind das ~1.057.000 Bytes — mehr als das 1-MiB-Limit,
  das manche Provider durchsetzen. Writer SOLLTEN daher bei `blockSize`
  ≤ 990 bleiben, sofern das Limit des Providers nicht als höher bekannt
  ist. Der theoretische Höchstfall verdoppelt die Nutzlast, kann bei
  AES-CTR-Ausgabe aber nicht eintreten.
- Der Dateiname im yEnc-Header MUSS der abgeleitete Artikelname
  (Abschnitt 2.2) plus `.zip` sein — nichtssagend, aber reproduzierbar und
  identisch mit dem Namen, auf den sich das PAR2-Set bezieht
  (Abschnitt 7.3). Ein Downloader benennt die zusammengesetzte Datei
  danach; fielen beide auseinander, beschriebe PAR2 eine Datei, die
  nirgends existiert. Das Subject SOLLTE denselben Namen tragen; der
  From-Header SOLLTE ein **konsistentes Pseudonym** sein — pro Artikel wechselnde Fantasie-Identitäten wirken wie
  Header-Fälschung und kollidieren mit üblichen Provider-AUPs.
- Es wird in genau **eine** Binary-Newsgroup gepostet (kein Crossposting).
- Nennt der Server in seiner `240`-Antwort eine **abweichende** Message-ID,
  MUSS der Poster den Upload als gescheitert behandeln und unter der
  nächsten Generation neu posten (Abschnitt 6). Die ID des Servers zu
  übernehmen — wie es allgemeine Uploader tun — machte das Segment dauerhaft
  unauffindbar, denn der Reader leitet IDs ausschließlich aus dem Token ab.
  Die beiden Hälften liegen dabei auf verschiedenen Ebenen: Der Poster MUSS
  den Fehlschlag melden und darf den Artikel **nicht** unter derselben
  Generation wiederholen — das ergäbe dieselbe ID und dasselbe Ergebnis. Die
  **Neuvergabe unter `gen+1` obliegt der aufrufenden Ebene**, denn sie setzt
  Zustand voraus, den ein einzelner Durchlauf nicht hat: Welche Generation
  als Nächstes frei ist, steht in der Bestandsaufnahme des Uploads
  (Abschnitt 6), nicht im laufenden POST. Ein Poster, der von sich aus
  `gen+1` wählte, überschriebe womöglich eine Generation, die anderswo
  bereits vergeben ist.
- Der Poster SOLLTE nach dem Upload jedes Segment per `STAT` verifizieren,
  und zwar gegen den **lesenden** Server. Provider trennen Post- und
  Lese-Hosts mitunter (Eweka etwa nimmt Artikel nur auf `post.eweka.nl` an,
  während `news.eweka.nl` nur liest); ein `STAT` gegen den Posting-Host
  besagt nichts darüber, was der Reader später sieht. Gemessene Propagation
  zwischen beiden: unter vier Sekunden.

---

## 6. Generationen (Wiederherstellungs-Byte in der Ableitung)

Message-IDs sind im Usenet global einmalig; ein Re-Post unter einer bereits
existierenden ID wird abgelehnt, solange der Artikel auf irgendeinem Peer
noch lebt. Deshalb enthält jede ID-Ableitung ein **Generations-Byte**
`gen` (uint8, initial 0):

- **Erst-Upload:** alle Segmente mit `gen = 0`.
- **Repair:** Ein verlorenes Segment `i` wird bytegleich rekonstruiert
  (aus PAR2) und unter `gen(i) + 1` neu gepostet. Nur das betroffene
  Segment wechselt die Generation; es wird kein Zustand gespeichert.
- **Reader:** Liefert `ARTICLE`/`STAT` für `(i, gen)` ein 430 (nicht
  vorhanden), probiert der Reader aufsteigend `gen+1, gen+2, …` bis
  Erfolg oder bis zum Limit `MAX_GEN` (empfohlen: 8, konfigurierbar).
  Erst danach gilt das Segment als verloren. Gefundene Generationen
  SOLLTEN pro Sitzung gecacht werden.
- Reparierte Segmente MÜSSEN bytegleich zum Original sein (gleicher
  Ciphertext). Es ist verboten, unter einer neuen Generation veränderten
  Inhalt zu posten.

Damit bleibt das System vollständig zustandslos: Token rein, alles andere
ist ableitbar oder erfragbar.

---

## 7. PAR2-Redundanz

Bei `recovery > 0` erzeugt der Writer PAR2-Daten über den **ZIP-Strom**
(Ciphertext; PAR2 selbst wird nicht zusätzlich verschlüsselt). Das Ergebnis
ist ein zweiter Bytestrom, der mit derselben `blockLen` gechunkt und unter
den `us3/par`-IDs gepostet wird. PAR2-Artikel unterliegen derselben
Generationslogik wie Segmente (Abschnitt 6).

Alles in diesem Abschnitt ist aus dem Token ableitbar. Der Reader DARF NICHT
darauf angewiesen sein, einen PAR2-Artikel zu holen, um die Gestalt des
PAR2-Stroms zu erfahren.

### 7.1 Blockgeometrie

Die PAR2-Blockgröße ist ein **Vielfaches** der Posting-Blockgröße:

```
M            = max(1, ⌈ zipSize / (4096 × blockLen) ⌉)
par2BlockLen = M × blockLen
nBlocks      = ⌈ zipSize / par2BlockLen ⌉
parityBlocks = ⌈ recovery × nBlocks / 100 ⌉
```

PAR2-Block `b` deckt die ZIP-Strom-Bytes
`[b·par2BlockLen, min((b+1)·par2BlockLen, zipSize))` ab; der letzte Block
wird für alle Hash- und Codierungszwecke mit Nullen auf `par2BlockLen`
aufgefüllt. Posting-Segment `i` liegt damit in PAR2-Block `⌊i / M⌋`.

Zwei Eigenschaften folgen konstruktionsbedingt und DÜRFEN NICHT als
eigenständige Nebenbedingungen behandelt werden:

- `nBlocks ≤ 4096`, sicher unterhalb des PAR2-Limits von **32.768**
  Quellblöcken (siehe 7.2). Ohne den Faktor `M` bräuchte ein 20-GB-Upload
  bei `blockSize = 375` genau 52.084 Blöcke — ein gültiges PAR2-Set
  existierte dann nicht.
- Der Rechenaufwand `zipSize × parityBlocks` bleibt gedeckelt. Er würde
  sonst quadratisch mit der Dateigröße wachsen; siehe 7.6.

`par2BlockLen` ist stets ein Vielfaches von 1024 und erfüllt damit die
PAR2-Vorgabe, dass die Slice-Größe ein Vielfaches von 4 sein muss.

### 7.2 Codierung

Codiert wird mit Reed-Solomon über GF(2¹⁶) nach PAR 2.0, Little Endian:

```
recovery[r] = Σ_b  const_b ^ r  ×  data_b        für r = 0 … parityBlocks−1
```

`const_b` ist die `b`-te Zweierpotenz der Ordnung 65535, also der `b`-te
Wert `2^n`, für den `n mod 3 ≠ 0`, `n mod 5 ≠ 0`, `n mod 17 ≠ 0` und
`n mod 257 ≠ 0` gilt. Genau 32.768 solcher Konstanten existieren — daher
rührt die Obergrenze der Quellblöcke. Der Exponent `r` wird in das
Recovery-Slice-Paket geschrieben.

### 7.3 Aufbau des Stroms

Der PAR2-Strom besteht aus Standard-PAR-2.0-Paketen (64-Byte-Header:
`PAR2\0PKT` ‖ Länge ‖ Paket-MD5 ‖ Recovery-Set-ID ‖ Typ). Er beschreibt
**genau eine Datei**, die den abgeleiteten Artikelnamen (Abschnitt 2.2)
plus `.zip` tragen MUSS — denselben Namen, den auch der yEnc-Kopf und der
Legacy-Export verwenden — mit Länge `zipSize` und Slice-Größe
`par2BlockLen`. Das Creator-Paket MUSS den ASCII-String `us3` enthalten.

Das **Index-Set** ist die Paketfolge

```
Main ‖ File Description ‖ Input File Slice Checksum ‖ Creator
```

und wird vor jeder Gruppe von 64 Recovery-Slice-Paketen wiederholt, dazu
einmal am Ende:

```
[Index-Set] [Recovery 0…63] [Index-Set] [Recovery 64…127] … [Index-Set]
```

Die Wiederholung ersetzt, was dateibasiertes PAR2 durch Kopieren der
Index-Pakete in jedes `volXX+YY.par2`-Volume erreicht: Der Verlust der ersten
Artikel des PAR2-Stroms darf nicht das ganze Set für Legacy-Tools unbrauchbar
machen. Ein US3-Reader ist darauf nicht angewiesen, da er alle Parameter aus
dem Token ableitet.

Anmerkung zum One-Pass-Betrieb: Die Recovery-Set-ID ist `MD5` des
Main-Paket-Bodys, der nur von Slice-Größe und File-ID abhängt, und die
File-ID ist `MD5(MD5-16k ‖ zipSize ‖ "stream.zip")`. Alle Eingaben stehen
nach den ersten 16.384 Bytes des ZIP-Stroms fest; Recovery-Slice-Pakete sind
also serialisierbar, ohne die Quelle zu puffern.

### 7.4 Größenformel

Mit dem obigen Aufbau liegen alle Paketlängen fest:

```
indexLen   = 360 + pad4(len(name)) + 20 × nBlocks
                                       # Main 92 + FileDesc (120 + pad4(name))
                                       #  + IFSC (80 + 20·nBlocks) + Creator 68
                                       # Name mit 17 Zeichen → pad4 = 20 → 380 + 20·nBlocks
recvLen    = 68 + par2BlockLen
nGroups    = ⌈ parityBlocks / 64 ⌉

par2Size   = (nGroups + 1) × indexLen + parityBlocks × recvLen
nPar2Seg   = ⌈ par2Size / blockLen ⌉
```

PAR2-Segment `j` trägt die PAR2-Strom-Bytes
`[j·blockLen, min((j+1)·blockLen, par2Size))`.

### 7.5 Reparatur

Ein verlorenes Posting-Segment zu rekonstruieren erfordert den PAR2-Block,
der es enthält, und einen PAR2-Block zu rekonstruieren erfordert **alle
übrigen Quellblöcke** — also den gesamten ZIP-Strom. Das ist Reed-Solomon
inhärent und keine Eigenschaft dieses Formats. Implementierungen SOLLTEN
Verluste aus Health-Checks sammeln und gebündelt reparieren statt einzeln.

### 7.6 Bewusste Abwägung

`par2BlockLen > blockLen` zu wählen vergröbert die Reparaturgranularität: Ein
einzelner verlorener Artikel verbraucht einen ganzen Recovery-Block. Bei
`M = 3` und 12 % Redundanz bleiben statt 12 % nur noch rund 4 % *verstreuter*
Artikelverluste reparierbar. Bei *geclustertem* Verlust — ganze Bereiche
fallen weg, der im Usenet häufigere Fall — bleiben es die vollen 12 %.

Die Alternative `par2BlockLen = blockLen` erhält die feinere Granularität,
macht die PAR2-Erzeugung aber zum Flaschenhals des Uploads: Für das
durchgerechnete Beispiel in Abschnitt 10 verdreifacht sie den Rechenaufwand
von 2,3 TB auf 6,9 TB GF-Operationen, und der Faktor wächst mit der
Dateigröße. Die Konstante 4096 in 7.1 ist der einzige Punkt, an dem diese
Abwägung eingestellt wird.

Der RAM-Bedarf wird von `M` *nicht* berührt: Der Recovery-Puffer beträgt
unabhängig davon `recovery` Prozent von `zipSize`.

---

## 8. Reader / Streaming-Ablauf

1. Token parsen, CRC prüfen, Version prüfen, `K` bilden.
2. Passwort, Salt → AES-Key lokal berechnen (kein Netzwerkzugriff).
3. `zipSize`, `nSeg`, `DATA_START` aus Token berechnen.
4. HTTP-Range-Request `[a, b]` auf das Video ⇒ ZIP-Offsets
   `[89+a, 89+b]` ⇒ Segmentindizes ⇒ Artikel parallel fetchen
   (Generationslogik), yEnc dekodieren, CRC prüfen.
5. Ciphertext-Bytes zusammensetzen, CTR-Zähler aus dem absoluten
   Ciphertext-Offset (`zipOff − 89`) berechnen, XOR, ausliefern.
   MIME-Type aus `extension`.
6. Sequentielle Zugriffsmuster SOLLTEN mit Read-Ahead (5–10 Segmente) und
   einem Segment-LRU-Cache bedient werden. Die Tiefe DARF die Zahl der
   verfügbaren Verbindungen NICHT überschreiten: Der Gewinn ist das
   Kleinere von beidem, und was darüber hinaus vorausgelesen wird, belegt
   die Verbindungen, auf die ein echter Zugriff dann wartet. Das Fenster
   wird bei jedem Zugriff aufgefüllt, nicht um eins weitergerückt — eine
   Tiefe von eins hält die Kette nie länger als ein Segment und wirkt wie
   gar kein Read-Ahead. Ein fehlgeschlagener Vorgriff ist **keine**
   Fehlstelle nach §8.1: Er wird verworfen, zählt nicht in den Lauf
   aufeinanderfolgender Verluste und wird nicht als gefüllte Stelle
   gemeldet — niemand hat diese Bytes angefordert.
   Zu beachten: Beim Streaming wird der ZIP-Header nie gelesen — der Reader
   springt direkt auf `89 + v`. Von den 284 Overhead-Bytes sind **274 allein
   aus dem Token ableitbar**: die 89 Bytes am Kopf (Local Header, Salt,
   Prüfwert) und die 185 Bytes am Schluss (Central Directory, ZIP64-EOCD,
   Locator, EOCD). Nur die 10 Bytes des AE-2-HMAC nicht, da dieser den
   gesamten Ciphertext abdeckt. Ein Reader DARF deshalb den Kopf von
   Segment 0 oder den Schluss des letzten Segments lokal erzeugen, wenn
   diese Artikel verloren sind und PAR2 sie nicht wiederherstellt; nur der
   Ciphertext-Anteil muss tatsächlich beschafft werden.
7. **Legacy-Export:** alle Segmente `0 … nSeg−1` in Reihenfolge
   konkatenieren ⇒ fertige ZIP-Datei, die den abgeleiteten Namen tragen MUSS;
   Passwort dem Nutzer anzeigen.

   Der PAR2-Strom DARF NICHT als eine einzige Datei geschrieben werden.
   Klassische Werkzeuge behandeln eine reine `.par2` als *Index*-Datei
   und weisen eine zurück, die Recovery-Pakete enthält — gopar bricht
   mit „recovery packets found in index file“ ab. Der Exporter liest
   die konkatenierten PAR2-Segmente deshalb paketweise und verteilt sie:

   - `<name>.zip.par2` — ein Index-Set, keine Recovery-Pakete;
   - `<name>.zip.volXX+YY.par2` — ein Index-Set plus Recovery-Pakete,
     exponentiell aufgeteilt (1, 2, 4, 8, … Blöcke), wobei `XX` der
     Exponent des ersten Blocks in der Datei ist und `YY` die Anzahl
     der Blöcke. Die letzte Datei darf kleiner sein.

   Dabei wird kein Paket neu gebaut, und der gepostete Strom bleibt
   unberührt — die Aufteilung existiert nur auf der Platte. Ein NZB
   DARF on-demand aus den abgeleiteten IDs synthetisiert werden, wird
   aber niemals gespeichert.

### 8.1 Fehlstellen beim Streaming

Ein Segment gilt als **endgültig verloren**, wenn der Server es in allen
Generationen `0 … MAX_GEN` als nicht vorhanden meldet (430) oder der dort
gefundene Artikel unbrauchbar ist (yEnc oder CRC). Ein Transportfehler —
Verbindungsabriss, Zeitüberschreitung oder eine Serverantwort, die kein 430
ist — ist **kein** Verlust und DARF NICHT wie einer behandelt werden.

Beim Streaming DARF ein Reader die Bytes eines endgültig verlorenen
Segments als Nullen ausliefern, statt die laufende Antwort abzubrechen. Er
MUSS jede gefüllte Stelle protokollieren, SOLLTE sich das verlorene Segment
für die Sitzung merken, statt die Generationssuche bei jedem Zugriff zu
wiederholen, und MUSS abbrechen, sobald mehr als eine
implementierungsdefinierte Zahl **aufeinanderfolgender** Segmente fehlt
(Empfehlung: 4).

Beim `fetch` und beim Legacy-Export (Punkt 7) DARF NICHT gefüllt werden:
Dort entstünde eine ZIP-Datei, die vollständig aussieht und erst beim
Entpacken am AE-2-HMAC scheitert. Ein verlorenes Segment MUSS den Export
scheitern lassen.

Begründung: Der Abbruch kostet den Nutzer mehr als die Lücke. Ein Player
wiederholt den Bereich, scheitert erneut und wertet das als Dateiende — der
Film endet an der Fehlstelle. AES-CTR verschleppt keine Fehler, der Schaden
bleibt also örtlich, und der HMAC wird beim Streaming ohnehin nie geprüft
(Punkt 6). Gemessen in `docs/spike-fehlstellen.md`.

### 8.2 Zugriff als Dateisystem

Ein Reader, der die Bibliothek über eine Dateisystem-Schnittstelle (FUSE)
ausliefert, MUSS einen Read, den er nicht bedienen kann, als Fehler melden
und DARF außer am Dateiende NICHT weniger Bytes liefern als angefordert. Ein
kurzer Read ist das Zeichen für das Dateiende: Ein Player, der einen
bekommt, hört dort auf und meldet Erfolg. Das Füllen eines endgültigen
Verlusts (§8.1) ist kein Fehler und wird als Inhalt ausgeliefert.

Ein solcher Reader DARF den Inhalt vom Betriebssystem ohne Zeitgrenze
zwischenspeichern lassen: Ein Token legt jedes Byte seiner Datei fest. Der
Schlüssel dieses Zwischenspeichers MUSS sich ändern, wenn der Eintrag unter
einem Pfad durch einen anderen Token ersetzt wird. Für die unter §8.1
gefüllten Nullen gilt diese Zusicherung nicht, deshalb kann gefüllter
Inhalt im Zwischenspeicher des Betriebssystems fortbestehen, bis er
verdrängt wird oder das Dateisystem erneut eingehängt wird — auch nachdem
eine Reparatur das Segment wieder verfügbar gemacht hat.

---

## 9. Sicherheitsbetrachtungen

### 9.1 MUSS-Regeln

1. **Ein Seed = ein Upload.** Unter einem Token DARF NICHT jemals anderer
   Plaintext verschlüsselt werden — auch nicht „dieselbe Datei, neu
   remuxt“. Salt und Passwort sind deterministisch; abweichender Plaintext
   unter gleichem `K` wäre CTR-Nonce-Reuse und bricht die Vertraulichkeit
   vollständig. Der Ingest MUSS pro Lauf einen frischen Seed erzeugen, und
   zwar **nach** allen inhaltsverändernden Schritten (Remux etc.).
2. Reparierte Segmente MÜSSEN bytegleich sein (Abschnitt 6).
3. Der Token-CRC MUSS vor jeder Verwendung geprüft werden.
4. Ein Token, dessen `extension` nicht aus genau drei Bytes aus `a–z` und
   `0–9` besteht, MUSS abgelehnt werden (Abschnitt 1.2), bevor die Endung
   in einen Datei- oder Pfadnamen eingeht.
5. Ein Token DARF NICHT in Logmeldungen, Fehlertexten oder beiläufigen
   Ausgaben an den Nutzer erscheinen — weder vollständig noch in einer
   Form, aus der es sich rekonstruieren lässt. Ausgenommen ist die
   Ausgabe eines Befehls, deren Zweck die Übergabe des Tokens an seinen
   Besitzer ist. Aus dem Token folgen Passwort und voller Zugriff
   (Abschnitt 9.2, Capability-Token), und ein Log überlebt die Sitzung,
   in der es geschrieben wurde.

### 9.2 Eigenschaften und bewusste Abwägungen

- **Capability-Token:** Das Token vereint Fundort und Schlüssel. Wer den
  String besitzt, besitzt das Video — das macht Sharing trivial (ein String
  genügt), verlangt aber, Tokens wie private Schlüssel zu behandeln. Der
  `us3`-Präfix macht geleakte Tokens zudem trivial greppbar.
- **Klartext-Fingerabdruck:** ZIP-Header sind prinzipbedingt unverschlüsselt.
  Segment 0 jedes US3-Uploads beginnt mit einem identischen, maschinell
  erkennbaren Muster (`PK\x03\x04`, Methode 99, `stream.xxx`,
  AES-Extrafeld). Das gefährdet nicht die Vertraulichkeit (Known Plaintext
  bricht AES-CTR nicht), wohl aber die Unauffälligkeit — der Preis der
  7-Zip-Kompatibilität. Für den PAR2-Strom gilt dasselbe: Seine Artikel
  beginnen mit `PAR2\0PKT`-Paketen, die die Datei `stream.zip` und den
  Creator `us3` benennen. Beides folgt zwangsläufig aus der
  Standardkonformität, und keines verknüpft die beiden Ströme
  miteinander — die Message-IDs bleiben unverkettbar (Abschnitt 3).
- **Öffentlichkeit:** Alle Artikel sind für jedermann abrufbar. Die
  Vertraulichkeit hängt ausschließlich an den 256 Bit Seed-Entropie.
- **Kein Vorwärtsgeheimnis pro Datei nötig:** Jedes Archiv hat einen
  eigenen Seed; die Kompromittierung eines Tokens betrifft genau ein Video.
- **Zeitfeld:** unsigned, gültig bis 2106; rein informativ.
- **Verfügbarkeit:** Usenet-Provider übernehmen keinerlei Haftung für
  Bestand oder Vollständigkeit geposteter Inhalte und können Inhalte
  jederzeit entfernen. US3 mit PAR2 + periodischem Health-Check mildert
  Artikelverlust, ersetzt aber keine unabhängige Zweitkopie.

---

## 10. Durchgerechnetes Beispiel

Token:

```
us3AdSwMr68w8mItcl_ujBrnYD7j4N1uqf_Pks3C5-VHAW6AAAkGAEAAAAYPXxqdwFta3YMJoGPIQ
```

Blob (55 Bytes, hex):

```
000000  01 D4 B0 32 BE BC C3 C9 88 B5 C9 7F BA 30 6B 9D
000010  80 FB 8F 83 75 BA A7 FF 3E 4B 37 0B 9F 95 1C 05
000020  BA 00 00 24 18 01 00 00 00 18 3D 7C 6A 77 01 6D
000030  6B 76 0C 26 81 8F 21
```

| Feld          | Rohwert                   | Bedeutung |
|---------------|---------------------------|-----------|
| version       | `01`                      | 1 |
| seed value    | `D4B032…951C05BA`         | 256-Bit-Zufall |
| file size     | `00 00 24 18 01 00 00 00` | 4.699.979.776 Bytes |
| creation time | `18 3D 7C 6A`             | 1786527000 = 2026-08-12 09:30:00 UTC |
| block size    | `77 01`                   | 375 ⇒ 384.000 Bytes |
| extension     | `6D 6B 76`                | `mkv` ⇒ Dateiname `stream.mkv` |
| recovery      | `0C`                      | 12 % PAR2 |
| crc-32        | `0C 26 81 8F 21`… → `21 8F 81 26` LE | `218F8126` |

Abgeleitete Werte (`K` = Blob-Bytes 0–50):

```
ZIP-Passwort : ozQ3csNcX6wgO1Mc37pkcWSRwUo75dkZVmHyoMwqQpE
AES-Salt     : BD5623602F144FB72A0FF89D978AE194

zipSize      : 4.699.979.776 + 284 = 4.699.980.060 Bytes
nSeg         : ⌈4.699.980.060 / 384.000⌉ = 12.240 Segmente
```

PAR2 (Abschnitt 7, `recovery` = 12):

```
M            : max(1, ⌈4.699.980.060 / (4096 × 384.000)⌉) = 3
par2BlockLen : 3 × 384.000 = 1.152.000 Bytes
nBlocks      : ⌈4.699.980.060 / 1.152.000⌉ = 4.080
parityBlocks : ⌈12 × 4.080 / 100⌉ = 490
indexLen     : 380 + 20 × 4.080 = 81.980 Bytes  (Name mit 17 Zeichen)
recvLen      : 68 + 1.152.000 = 1.152.068 Bytes
nGroups      : ⌈490 / 64⌉ = 8
par2Size     : 9 × 81.980 + 490 × 1.152.068 = 565.251.140 Bytes
nPar2Seg     : ⌈565.251.140 / 384.000⌉ = 1.473 Segmente

Recovery-Puffer im Writer : 490 × 1.152.000 = 538 MiB
```

Message-IDs:

```
Segment 0, gen 0 : <tenerflxp2jcdw3sst47ehsm@qfp3gwjv.net>
Segment 1, gen 0 : <h6gz6lanzkx5kcqcksektijr@tnxa4k6u.org>
Segment 2, gen 0 : <ejyx27oabacvm6w33fqflkqi@d2bpzdct.net>
Segment 1, gen 1 : <q2yzzia57qsfpckefqtkamzk@mkltmetf.org>   (Repair-Repost)
PAR2 0,    gen 0 : <hdtxxuz3eel6v64zcjxexfro@qrr5y5ix.org>
```

Alle Werte sind mit den Formeln dieser Spezifikation reproduzierbar und
eignen sich als Golden-Test-Vektoren für Implementierungen.

---

## 11. Weitergabe: NZBLNK

Ein Token allein benennt den Inhalt nicht — es trägt Größe, Blockgröße und
drei Zeichen Endung, aber keinen Titel (Abschnitt 1.1). Damit ein Empfänger
weiß, was er bekommt, wird ein US3-Inhalt als **NZBLNK-URI**
(<https://nzblnk.info/>) weitergegeben:

```
nzblnk:?t=Bergwanderung+2026&h=us3AQFTyRl_MyxolNxIdFOprr8od6hmu7TQq01UE91UsXQ4wMYtAAAAAAD_Xp5qdwFta3YKDNF8cg
```

Die Parameter folgen den Regeln eines Query-Strings nach RFC 3986; ihre
Reihenfolge ist bedeutungslos. Das Schema wird mit klassischen NZBLNK-Links
geteilt und nicht durch ein eigenes ersetzt — ein Empfänger, der beide
bedient, unterscheidet sie am Inhalt von `h`.

### 11.1 Parameter

| Parameter | | Bedeutung bei US3 |
|---|---|---|
| `h` | MUSS | Das vollständige Token, mit `us3`-Präfix. Bei klassischem NZBLNK steht hier ein Header-Fragment; bei US3 tritt das Token an dessen Stelle, weil aus ihm alle Message-IDs folgen (Abschnitt 3). |
| `t` | MUSS | Titel **ohne** Endung, im Sinne von NZBLNK ein Etikett. Die Endung kommt aus dem Token. |
| `p` | DARF | Wird gelesen und verworfen: Das Passwort ist aus dem Token abgeleitet (Abschnitt 2.2). Ein Sender SOLLTE `p` nicht setzen — es wäre redundant und legte nahe, das Token genüge nicht allein. |
| `g` | DARF | Wird gelesen und verworfen: Die Fundorte hängen an den Message-IDs, nicht an einer Gruppe (Abschnitt 3). |

Weitere Parameter MÜSSEN ignoriert statt zurückgewiesen werden.

Das Token selbst ist base64url und besteht nur aus `A-Za-z0-9-_`, braucht
also keine Prozentkodierung. Im Titel steht `+` oder `%20` für das
Leerzeichen.

### 11.2 Erkennung

Ein Empfänger erkennt einen US3-Link daran, dass `h` mit `us3` beginnt
**und** sich nach Abschnitt 9.1, Regeln 3 und 4 parsen lässt — Länge,
kanonische base64url-Kodierung, CRC, Version und Endung. Die CRC macht die Erkennung
belastbar: Ein fremdes Header-Fragment scheitert entweder schon am Präfix
oder spätestens an der Prüfsumme.

Ein Empfänger, der das Schema `nzblnk:` für sich beansprucht, SOLLTE einen
Link, dessen `h` kein gültiges US3-Token ist, unverändert an den zuvor
registrierten Handler weiterreichen, statt ihn zu verwerfen. Pro System kann
nur ein Programm ein Schema beanspruchen; wer das nicht beachtet, nimmt dem
Nutzer die klassischen Links.

### 11.3 Titel als Dateiname

Ein Sender SOLLTE `t` als schlichtes Etikett schicken — einen Titel, keinen
Pfad. Wo ein Inhalt beim Empfänger abgelegt wird, entscheidet der
Empfänger, nicht der Sender.

Ein Empfänger übernimmt `t` **unverändert** als Dateinamen und hängt die
Endung aus dem Token an. Er räumt nicht auf: Punkte, Bindestriche und
Schreibweisen bleiben, wie sie ankommen, weil jede Umformung an einer Stelle
rät, an der Raten schiefgehen kann.

```
t=Bergwanderung.2026.4K-cam2
  ⇒   Bergwanderung.2026.4K-cam2.mkv
```

Ein Empfänger DARF ein `/` im Titel als Trenner von Verzeichnisebenen
deuten. Das ist eine Freiheit für seine eigene Ablage — etwa wenn er den
Titel in seiner Kopie des Links von Hand zu `Reisen/Bergwanderung 2026/Drohnenflug
Teil 2` ändert —, keine Erwartung an den Sender. Deutet er es so, MUSS er
die Ebenen `.` und `..` verwerfen, bevor er den Titel als Pfad verwendet.

Ein Empfänger DARF Zeichen ersetzen, die sein Dateisystem nicht trägt. Ein
leerer Titel oder einer, der nach dieser Behandlung leer ist, macht den Link
ungültig.

### 11.4 Vertraulichkeit

Der Link trägt das Token im Klartext und damit den vollständigen Zugriff:
Fundort *und* Entschlüsselung (Abschnitt 9). Er ist so vertraulich wie die
Datei selbst. Ein Link gehört deshalb nicht in Suchmaschinen, Logdateien
oder öffentliche Verläufe — und ein Empfänger SOLLTE ihn nicht dorthin
weitergeben, wo er dauerhaft sichtbar bleibt.
