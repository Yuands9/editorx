"""One CPU translation per process; all model memory is released on exit."""
import ctypes
import json
import os
import re
import sys
import time
from ctypes import wintypes

started = time.perf_counter()
sys.stdout.reconfigure(encoding="utf-8")
request = json.load(sys.stdin.buffer)

import ctranslate2
import sentencepiece

model = request["model"]
tokenizer = sentencepiece.SentencePieceProcessor(model_file=os.path.join(model, "sentencepiece.model"))
translator = ctranslate2.Translator(os.path.join(model, "model"), device="cpu", compute_type="int8", inter_threads=1, intra_threads=2)
loaded = time.perf_counter()

# Keep newlines, placeholders, code spans and link destinations outside inference.
# Long lines are chunked explicitly; CTranslate2 must not silently truncate them.
output = []
for part in request["parts"]:
    if part["keep"]:
        output.append(part["text"])
        continue
    for line in re.split(r"(\r\n|\r|\n)", part["text"]):
        if not any(character.isalpha() for character in line):
            output.append(line)
            continue
        leading = line[:len(line) - len(line.lstrip())]
        trailing = line[len(line.rstrip()):]
        # Sentence boundaries reduce work on paragraphs without another NLP model.
        sentences = re.split(r"(?<=[。！？!?])|(?<=\.) (?=[A-Z])", line.strip())
        chunks = []
        for sentence in sentences:
            tokens = tokenizer.encode(sentence, out_type=str)
            chunks.extend(tokens[index:index + 256] for index in range(0, len(tokens), 256))
        results = translator.translate_batch(chunks, beam_size=2, max_batch_size=8,
                                             max_input_length=0, max_decoding_length=512,
                                             replace_unknowns=True)
        separator = " " if request["target"] == "en" else ""
        # Argos also replaces residual SentencePiece space markers after decoding.
        translated = separator.join(tokenizer.decode_pieces(result.hypotheses[0]).replace("▁", " ").strip() for result in results)

        if request["target"] == "zh-cn":
            translated = re.sub(r" +([，。！？；：])", r"\1", translated)
        if not translated:
            raise ValueError("The model returned an empty translation")
        output.append(leading + translated + trailing)

finished = time.perf_counter()

class MemoryCounters(ctypes.Structure):
    _fields_ = [("cb", wintypes.DWORD), ("PageFaultCount", wintypes.DWORD)] + [
        (name, ctypes.c_size_t) for name in ("PeakWorkingSetSize", "WorkingSetSize",
        "QuotaPeakPagedPoolUsage", "QuotaPagedPoolUsage", "QuotaPeakNonPagedPoolUsage",
        "QuotaNonPagedPoolUsage", "PagefileUsage", "PeakPagefileUsage", "PrivateUsage")]

memory = MemoryCounters()
memory.cb = ctypes.sizeof(memory)
ctypes.windll.kernel32.GetCurrentProcess.restype = wintypes.HANDLE
ctypes.windll.psapi.GetProcessMemoryInfo.argtypes = [wintypes.HANDLE, ctypes.c_void_p, wintypes.DWORD]
if not ctypes.windll.psapi.GetProcessMemoryInfo(ctypes.windll.kernel32.GetCurrentProcess(), ctypes.byref(memory), memory.cb):
    raise ctypes.WinError()
print(json.dumps({"text": "".join(output), "loadMs": round((loaded - started) * 1000, 2),
                  "translateMs": round((finished - loaded) * 1000, 2),
                  "peakWorkingSet": memory.PeakWorkingSetSize,
                  "privateBytes": memory.PrivateUsage}, ensure_ascii=False))
