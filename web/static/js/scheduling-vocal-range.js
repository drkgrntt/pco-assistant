// scheduling-vocal-range.js - the vocal range reference-tone player (real
// piano samples with a synthesized Web Audio sine-tone fallback,
// playVocalTone/playVocalNoteFromInput) and the Song Vocal Range page's
// key/octave live transposition preview (renderVocalRangePreview and its
// supporting pitch-class/semitone math, mirroring key_engine.go). Split out
// of the former single scheduling.js - see scheduling-global.js's header
// for the same note.

// --- Vocal range reference tone -----------------------------------------
//
// Step 2 ("later") of docs/ideas.md's "Vocal range per vocalist" bullet:
// tap-to-hear a reference tone for a stored note, so a director unsure
// what a "G4" actually sounds like doesn't have to work it out by ear or
// on an instrument. Originally built as a synthesized tone only (a plain
// sine OscillatorNode via the Web Audio API) - no asset files needed, but
// a synthesized sine wave is a thinner, less musical reference than an
// actual piano note. Now that assets/piano-sound-samples/ exists (a real
// chromatic E2-C6 piano recording set, MIT-licensed, see that folder's
// MAPPING.md, served unauthenticated at /piano-samples/ - see routes.go)
// a real recorded sample is the primary sound: a genuine piano note is a
// more faithful and pleasant reference tone than a synthesized sine, and
// E2-C6 already covers typical choir range (that folder's own MAPPING.md).
// The original synthesis code is kept, unchanged, as the fallback for the
// rare note outside that sampled range (very low bass or very high
// soprano/countertenor notes) - see playVocalTone below. See
// docs/plans/in-progress/vocal-range-plan.md's status note for where this
// landed.

// vocalAbsoluteSemitone combines a pitch class (0-11) + octave into one
// internally-consistent absolute integer - octave*12 + pitchClass,
// mirroring scheduling.AbsoluteSemitone exactly (Go,
// internal/scheduling/key_engine.go) so a played tone always matches what
// that same pitch class/octave pair means when stored. Shared by
// vocalNoteFrequency (the synthesis fallback) and playVocalTone's
// sampled-range bounds check below, so the formula lives in exactly one
// place on the JS side rather than being hardcoded twice.
function vocalAbsoluteSemitone(pitchClass, octave) {
  return octave * 12 + pitchClass;
}

// vocalNoteFrequency converts a pitch class (0-11) + octave into a
// frequency in Hz, standard A440 equal temperament - backs the synthesis
// fallback only (see playVocalTone). A4 (octave 4, pitch class 9) is
// absolute semitone 57, which is where the formula's "-57" reference
// point comes from.
function vocalNoteFrequency(pitchClass, octave) {
  var absoluteSemitone = vocalAbsoluteSemitone(pitchClass, octave);
  return 440 * Math.pow(2, (absoluteSemitone - 57) / 12);
}

// vocalSampleNoteLetters is vocalNotePitchClasses' inverse - pitch class
// (0-11) -> the sharp-only filename spelling assets/piano-sound-samples/
// uses (e.g. pitch class 1 -> "Cs", matching Cs4.mp3). That pack only
// recorded sharp spellings (see its own MAPPING.md), so this never needs
// a flat variant.
var vocalSampleNoteLetters = [
  "C", "Cs", "D", "Ds", "E", "F", "Fs", "G", "Gs", "A", "As", "B",
];

// vocalSampleMinAbsoluteSemitone/vocalSampleMaxAbsoluteSemitone bound the
// real recordings available under assets/piano-sound-samples/ - a
// chromatic run from E2 through C6 (see that folder's MAPPING.md), chosen
// to cover typical choir range. A pitch class/octave pair outside this
// span has no recorded sample and falls back to synthesis (see
// playVocalTone).
var vocalSampleMinAbsoluteSemitone = vocalAbsoluteSemitone(4, 2); // E2
var vocalSampleMaxAbsoluteSemitone = vocalAbsoluteSemitone(0, 6); // C6

// vocalSampleURL builds a piano-sample file's URL for a pitch class +
// octave - e.g. pitch class 1, octave 4 -> "/piano-samples/Cs4.mp3". The
// mount itself is registered in routes.go (s.App.Static) and exempted
// from RequireAuth alongside /static/* (see publicPrefixes,
// internal/server/middleware/auth.go) - a sound file carries no
// auth-relevant content, same trust level as CSS/JS already served
// openly.
function vocalSampleURL(pitchClass, octave) {
  return "/piano-samples/" + vocalSampleNoteLetters[pitchClass] + octave + ".mp3";
}

// vocalNotePitchClasses mirrors keyPitchClasses/vocalNoteRe's letter ->
// pitch class table (internal/server/controllers/vocal_range.go) - kept
// as a separate JS-side table rather than shared with the Go code (no
// realistic way to share a lookup table across that language boundary),
// but must stay numerically identical to it.
var vocalNotePitchClasses = { C: 0, D: 2, E: 4, F: 5, G: 7, A: 9, B: 11 };

// vocalNoteTextRe mirrors parseVocalNote's vocalNoteRe exactly (Go,
// internal/server/controllers/vocal_range.go): a note letter A-G, an
// optional single sharp/flat accidental (ASCII or Unicode), then a
// signed integer octave.
var vocalNoteTextRe = /^([A-Ga-g])(#|♯|b|♭)?(-?[0-9]+)$/;

// parseVocalNoteText parses the same note+octave text format
// parseVocalNote (Go) accepts, returning {pitchClass, octave} or null if
// it doesn't parse - so "hear it" always matches what would actually be
// saved, and a not-yet-valid in-progress input (still being typed) just
// silently does nothing rather than erroring.
function parseVocalNoteText(text) {
  if (typeof text !== "string") {
    return null;
  }
  var m = vocalNoteTextRe.exec(text.trim());
  if (!m) {
    return null;
  }
  var pitchClass = vocalNotePitchClasses[m[1].toUpperCase()];
  if (pitchClass === undefined) {
    return null;
  }
  if (m[2] === "#" || m[2] === "♯") {
    pitchClass += 1;
  } else if (m[2] === "b" || m[2] === "♭") {
    pitchClass -= 1;
  }
  pitchClass = ((pitchClass % 12) + 12) % 12;

  var octave = parseInt(m[3], 10);
  if (isNaN(octave)) {
    return null;
  }
  return { pitchClass: pitchClass, octave: octave };
}

// vocalToneAudioContext is a single lazily-created AudioContext, reused
// across every played tone rather than one per call - browsers require
// it to originate from a user gesture anyway (a button click satisfies
// that), and creating a fresh one per play would be wasteful and can hit
// browser-imposed limits on concurrent contexts.
var vocalToneAudioContext = null;

// playVocalToneSynthesized plays a short sine reference tone at the given
// pitch class/octave's frequency (~0.8s, with a brief linear attack/
// release envelope on a GainNode to avoid a click/pop at start/stop) -
// the original (and, before the piano sample set existed, only)
// implementation of playVocalTone, kept unchanged and now used only as
// the fallback for a note outside the sampled range (see playVocalTone).
// Wrapped entirely in try/catch: a browser blocking audio (autoplay
// policy, no AudioContext support at all) should mean the button silently
// does nothing, never a broken page or a thrown error.
function playVocalToneSynthesized(pitchClass, octave) {
  try {
    if (!vocalToneAudioContext) {
      var AudioContextCtor = window.AudioContext || window.webkitAudioContext;
      if (!AudioContextCtor) {
        return;
      }
      vocalToneAudioContext = new AudioContextCtor();
    }
    var ctx = vocalToneAudioContext;
    if (ctx.state === "suspended") {
      ctx.resume();
    }

    var frequency = vocalNoteFrequency(pitchClass, octave);
    var now = ctx.currentTime;
    var attack = 0.02;
    var duration = 0.8;
    var peakGain = 0.3;

    var oscillator = ctx.createOscillator();
    oscillator.type = "sine";
    oscillator.frequency.setValueAtTime(frequency, now);

    var gainNode = ctx.createGain();
    gainNode.gain.setValueAtTime(0, now);
    gainNode.gain.linearRampToValueAtTime(peakGain, now + attack);
    gainNode.gain.setValueAtTime(peakGain, now + duration - attack);
    gainNode.gain.linearRampToValueAtTime(0, now + duration);

    oscillator.connect(gainNode);
    gainNode.connect(ctx.destination);

    oscillator.start(now);
    oscillator.stop(now + duration);
  } catch (err) {
    // Autoplay blocked, no Web Audio support, etc. - silently do nothing.
  }
}

// playVocalTone plays a reference tone for the given pitch class/octave -
// a real recorded piano note (assets/piano-sound-samples/, served at
// /piano-samples/) when one exists for it, falling back to
// playVocalToneSynthesized's sine tone otherwise (a note outside the
// sampled E2-C6 range - rare in practice, since that range already covers
// typical choir range). Wrapped entirely in try/catch, same reasoning as
// playVocalToneSynthesized: a blocked/failed play should stay silent,
// never a broken page or a thrown error - any failure to even start the
// sample playback falls through to the synthesized fallback rather than
// just going silent, so a director always hears *something*.
function playVocalTone(pitchClass, octave) {
  var absoluteSemitone = vocalAbsoluteSemitone(pitchClass, octave);
  if (
    absoluteSemitone >= vocalSampleMinAbsoluteSemitone &&
    absoluteSemitone <= vocalSampleMaxAbsoluteSemitone
  ) {
    try {
      var audio = new Audio(vocalSampleURL(pitchClass, octave));
      audio.play().catch(function () {
        // Autoplay blocked, network error, etc. - silently do nothing
        // (matches playVocalToneSynthesized's own silent-failure
        // convention). Deliberately not falling back to synthesis here:
        // Audio.play()'s promise rejection fires asynchronously, well
        // after this function has already returned, so a fallback here
        // would risk playing a late, unexpected tone after the button
        // press moment has passed.
      });
      return;
    } catch (err) {
      // Synchronous construction/playback failure - fall through to
      // synthesis below rather than staying silent.
    }
  }
  playVocalToneSynthesized(pitchClass, octave);
}

// playVocalNoteFromInput reads a text input's current value (the add-
// range form's still-being-typed Lowest/Highest note fields), parses it,
// and plays it if valid - does nothing otherwise, so previewing a note
// before save never errors on an in-progress, not-yet-valid value.
function playVocalNoteFromInput(inputId) {
  var input = document.getElementById(inputId);
  if (!input) {
    return;
  }
  var parsed = parseVocalNoteText(input.value);
  if (!parsed) {
    return;
  }
  playVocalTone(parsed.pitchClass, parsed.octave);
}

// --- Song vocal range key/octave preview --------------------------------
//
// docs/issues.md: "I feel like we're missing the ability to change the
// relative key of an existing range to hear what the modulated range
// would be. Could also be helpful to do things like bring it down an
// octave to differentiate for male/female type of thing." Preview-only -
// this never changes what's actually stored (song_vocal_range.templ's "As
// measured"/"Current key" lines, or the underlying DB row); it just
// recomputes a transposed low/high pair from the row's already-saved
// reference key + exact low/high notes, expressed as absolute semitones
// (the data-reference-key/data-low-abs/data-high-abs attributes
// vocalRangePreviewControls, song_vocal_range.templ, renders per row) and
// plays it - the same "hear it before it's committed" spirit as
// playVocalNoteFromInput above, just previewing a transposition of an
// already-saved row instead of an in-progress form value.
//
// The semitone arithmetic below deliberately mirrors
// internal/scheduling/key_engine.go and EffectiveVocalRange
// (song_vocal_range.go) function-for-function (vocalKeyPitchClass ~
// ParseKeyPitchClass, vocalSignedSemitoneDelta ~ SignedSemitoneDelta,
// vocalPitchClassAndOctave ~ PitchClassAndOctave) rather than
// round-tripping through htmx on every key keystroke/octave click - same
// "duplicate the primitives, keep them numerically identical" call this
// file already makes for vocalAbsoluteSemitone/vocalNoteFrequency
// mirroring AbsoluteSemitone above. If key_engine.go's math ever changes,
// these need updating to match. No tonic-anchor octave to duplicate here
// any more (migration 00043 removed that whole concept from
// EffectiveVocalRange server-side) - just the saved low/high absolute
// semitones, shifted by the same signed delta the server applies.

// vocalNoteDisplayNames mirrors vocalNoteDisplayNames (Go,
// internal/views/scheduling/vocal_range.templ) - sharp spelling only, a
// computed preview note has one canonical display form same as a stored
// one does.
var vocalNoteDisplayNames = [
  "C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B",
];

// vocalNoteDisplayText mirrors formatVocalNoteDisplay (Go, vocal_range
// .templ).
function vocalNoteDisplayText(pitchClass, octave) {
  return vocalNoteDisplayNames[((pitchClass % 12) + 12) % 12] + octave;
}

// vocalKeyPitchClass parses a bare key string ("G", "Bb", "F#m") into a
// 0-11 pitch class, mirroring ParseKeyPitchClass (Go,
// internal/scheduling/key_engine.go) exactly, including that function's
// two quirks: major/minor quality (a trailing lowercase "m") is stripped
// and otherwise ignored entirely, and the accidental character ("#"/"♯"/
// "b"/"♭") is matched case-sensitively, unmodified, against whatever
// follows the (uppercased) letter. Reuses vocalNotePitchClasses, the same
// letter -> pitch-class table parseVocalNoteText already uses for note
// parsing - see that table's own doc comment. Returns null for anything
// that doesn't parse, the same "still being typed" tolerance
// parseVocalNoteText already applies.
function vocalKeyPitchClass(text) {
  if (typeof text !== "string") {
    return null;
  }
  var key = text.trim();
  if (key.slice(-1) === "m") {
    key = key.slice(0, -1);
  }
  if (key === "") {
    return null;
  }

  var pitchClass = vocalNotePitchClasses[key[0].toUpperCase()];
  if (pitchClass === undefined) {
    return null;
  }

  var accidental = key.slice(1);
  if (accidental === "#" || accidental === "♯") {
    pitchClass += 1;
  } else if (accidental === "b" || accidental === "♭") {
    pitchClass -= 1;
  } else if (accidental !== "") {
    return null;
  }

  return ((pitchClass % 12) + 12) % 12;
}

// vocalSignedSemitoneDelta mirrors SignedSemitoneDelta (Go,
// key_engine.go) exactly - the minimal SIGNED shift (-6..6) from pitch
// class fromPC to pitch class toPC, a tie at exactly 6 resolving to +6.
function vocalSignedSemitoneDelta(fromPC, toPC) {
  var delta = (((toPC - fromPC) % 12) + 12) % 12;
  if (delta > 6) {
    delta -= 12;
  }
  return delta;
}

// vocalPitchClassAndOctave mirrors PitchClassAndOctave (Go,
// key_engine.go) exactly - AbsoluteSemitone's inverse, recovering the
// 0-11 pitch class and octave (which may be negative) that combine back
// into a given absolute semitone value.
function vocalPitchClassAndOctave(absoluteSemitone) {
  var pitchClass = ((absoluteSemitone % 12) + 12) % 12;
  var octave = (absoluteSemitone - pitchClass) / 12;
  return { pitchClass: pitchClass, octave: octave };
}

// vocalRangePreviewContainer finds the .vocal-range-preview wrapper
// (vocalRangePreviewControls, song_vocal_range.templ) an event originated
// inside - both entry points below (the key input and the two octave
// buttons) are nested inside exactly one of these per row.
function vocalRangePreviewContainer(el) {
  return el.closest(".vocal-range-preview");
}

// shiftVocalRangePreviewOctave bumps a row's preview octave shift (stored
// on the container's own data-octave-shift attribute, so each row's shift
// is independent) by delta (-1 or +1, an octave down/up), updates the
// small shift indicator next to the buttons, and re-renders the preview
// line with the new shift applied.
function shiftVocalRangePreviewOctave(button, delta) {
  var container = vocalRangePreviewContainer(button);
  if (!container) {
    return;
  }

  var shift = parseInt(container.getAttribute("data-octave-shift"), 10);
  if (isNaN(shift)) {
    shift = 0;
  }
  shift += delta;
  container.setAttribute("data-octave-shift", String(shift));

  var shiftLabel = container.querySelector(".vocal-range-preview-shift");
  if (shiftLabel) {
    shiftLabel.textContent =
      shift === 0 ? "" : (shift > 0 ? "+" + shift : String(shift)) + " 8ve";
  }

  renderVocalRangePreview(container.querySelector(".vocal-range-preview-key-input"));
}

// renderVocalRangePreview recomputes and renders one row's preview line
// from its container's data-reference-key/data-low-abs/data-high-abs (r's
// own stored values, see vocalRangePreviewControls) plus whatever key text
// is currently in the input and the container's current octave shift. An
// empty key input falls back to the row's own
// reference key - so the octave buttons alone (no key ever typed) still
// preview something, the "bring it down an octave... male/female" use
// case docs/issues.md describes. Hides the preview line entirely (quiet
// skip, this codebase's established convention - e.g.
// keyTransitionWarnings' unparseable-key skip) when the key text doesn't
// parse.
function renderVocalRangePreview(input) {
  var container = vocalRangePreviewContainer(input);
  if (!container) {
    return;
  }
  var line = container.querySelector(".vocal-range-preview-line");
  if (!line) {
    return;
  }

  var referenceKey = container.getAttribute("data-reference-key") || "";
  var lowAbs = parseInt(container.getAttribute("data-low-abs"), 10);
  var highAbs = parseInt(container.getAttribute("data-high-abs"), 10);
  var octaveShift = parseInt(container.getAttribute("data-octave-shift"), 10);
  if (isNaN(octaveShift)) {
    octaveShift = 0;
  }

  var keyText = input.value.trim() || referenceKey;
  var targetPitchClass = vocalKeyPitchClass(keyText);
  var referencePitchClass = vocalKeyPitchClass(referenceKey);

  if (
    targetPitchClass === null ||
    referencePitchClass === null ||
    isNaN(lowAbs) ||
    isNaN(highAbs)
  ) {
    line.hidden = true;
    line.innerHTML = "";
    return;
  }

  var delta = vocalSignedSemitoneDelta(referencePitchClass, targetPitchClass);
  var octaveOffsetSemitones = octaveShift * 12;

  var low = vocalPitchClassAndOctave(lowAbs + delta + octaveOffsetSemitones);
  var high = vocalPitchClassAndOctave(highAbs + delta + octaveOffsetSemitones);

  line.hidden = false;
  line.innerHTML =
    "Preview (" + keyText + "): " +
    vocalNoteDisplayText(low.pitchClass, low.octave) +
    " " + vocalRangePreviewPlayButtonHTML("lowest", low.pitchClass, low.octave) +
    " - " +
    vocalNoteDisplayText(high.pitchClass, high.octave) +
    " " + vocalRangePreviewPlayButtonHTML("highest", high.pitchClass, high.octave);
}

// vocalRangePreviewPlayButtonHTML builds one tap-to-hear button for a
// preview line's dynamically-computed note. Deliberately matches
// IconPlay's exact SVG markup (internal/views/components/icons.templ) and
// .icon-btn's styling by hand, so a client-side-generated button looks
// identical to every server-rendered play button on this page - see this
// section's own doc comment for why duplicating rather than sharing a
// template is the right call here. label is always one of this file's own
// two literal strings ("lowest"/"highest") and pitchClass/octave are
// always plain integers - no untrusted input ever reaches this string, so
// building it via concatenation rather than DOM APIs is safe.
function vocalRangePreviewPlayButtonHTML(label, pitchClass, octave) {
  return (
    '<button type="button" class="icon-btn" aria-label="Play previewed ' +
    label + ' note" title="Play previewed ' + label + ' note" ' +
    'onclick="playVocalTone(' + pitchClass + ', ' + octave + ')">' +
    '<svg class="icon" width="16" height="16" viewBox="0 0 24 24" fill="currentColor">' +
    '<path d="M6 4l14 8-14 8V4Z"></path></svg>' +
    '</button>'
  );
}
