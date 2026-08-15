// GolfTrack's shared frontend glue: the auth cookie, the API helper, and the
// small Alpine components the pages share.
//
// This is the file that replaces Django's CSRF plumbing. Requests are
// authenticated with `Authorization: <token>`, read from the same `pb_auth`
// cookie the server reads to gate a page render (AUTH.md § "For the frontend"),
// so there is no CSRF token to fetch and no hidden form field to post.
(function () {
  'use strict';

  var COOKIE = 'pb_auth';

  function readCookie(name) {
    var parts = document.cookie ? document.cookie.split('; ') : [];
    for (var i = 0; i < parts.length; i++) {
      var pair = parts[i];
      var eq = pair.indexOf('=');
      if (eq > -1 && pair.slice(0, eq) === name) {
        return decodeURIComponent(pair.slice(eq + 1));
      }
    }
    return '';
  }

  // token pulls the JWT out of the cookie the SDK wrote. The cookie holds
  // `{"token": …, "record": {…}}`; only the token is of any use here, and the
  // server ignores the record half for the same reason — it is client-writable.
  function token() {
    var raw = readCookie(COOKIE);
    if (!raw) return '';
    try {
      return JSON.parse(raw).token || '';
    } catch (e) {
      return '';
    }
  }

  function clearToken() {
    document.cookie = COOKIE + '=; Path=/; Max-Age=0; SameSite=Lax';
    purgeOfflineData();
  }

  // The service worker caches authenticated, personalized HTML (including the
  // round JSON embedded in the play page) and round-play.js queues pending
  // shots in localStorage. Both must be purged on sign-out so a subsequent
  // user on this device cannot see the previous golfer's cached data.
  function purgeOfflineData() {
    try {
      for (var i = localStorage.length - 1; i >= 0; i--) {
        var key = localStorage.key(i);
        if (key && key.indexOf('golftrack:offline:') === 0) {
          localStorage.removeItem(key);
        }
      }
    } catch (e) {
      // localStorage may be unavailable (private browsing); nothing to purge.
    }
    if (navigator.serviceWorker && navigator.serviceWorker.controller) {
      navigator.serviceWorker.controller.postMessage({ type: 'clear-cache' });
    }
  }

  // --- Offline queue --------------------------------------------------------
  //
  // Shots taken without a connection are queued in localStorage under
  // `golftrack:offline:<roundId>`. Replaying them lives here rather than in the
  // play page's Alpine component so a golfer who closed the app or walked back
  // to the home or review page still drains the queue as soon as the connection
  // returns — otherwise those shots would be lost the moment the round is
  // completed and the server's completed-round guard starts rejecting them.
  var QUEUE_PREFIX = 'golftrack:offline:';
  var draining = null;

  function readQueue(key) {
    try {
      var raw = localStorage.getItem(key);
      var ops = raw ? JSON.parse(raw) : [];
      return Array.isArray(ops) ? ops : [];
    } catch (e) {
      return [];
    }
  }

  function writeQueue(key, ops) {
    try {
      if (ops && ops.length > 0) {
        localStorage.setItem(key, JSON.stringify(ops));
      } else {
        localStorage.removeItem(key);
      }
    } catch (e) {
      // localStorage may be unavailable (private browsing); nothing to persist.
    }
  }

  function queueKeys() {
    var keys = [];
    try {
      for (var i = 0; i < localStorage.length; i++) {
        var key = localStorage.key(i);
        if (key && key.indexOf(QUEUE_PREFIX) === 0) keys.push(key);
      }
    } catch (e) {
      return [];
    }
    return keys;
  }

  // drainQueues replays every round's queued operations in order. Concurrent
  // callers (the online event and the play page) share the one in-flight run so
  // no operation is sent twice.
  function drainQueues() {
    if (draining) return draining;
    var done = function () { draining = null; };
    draining = runDrain().then(done, done);
    return draining;
  }

  async function runDrain() {
    try {
      if (!navigator.onLine || !token()) return;
      var keys = queueKeys();
      for (var k = 0; k < keys.length; k++) {
        var key = keys[k];
        var roundId = key.slice(QUEUE_PREFIX.length);
        // Re-read the queue on every step so operations the play page appends
        // while the drain runs are picked up rather than overwritten.
        for (;;) {
          var ops = readQueue(key);
          if (ops.length === 0) break;
          var op = ops[0];
          if (op && op.op === 'addShot') {
            await api(
              '/api/rounds/' + roundId + '/holes/' + op.holeNumber + '/shots',
              { method: 'POST', body: JSON.stringify({ club: op.club }) }
            );
          }
          var latest = readQueue(key);
          if (latest.length === 0 || latest[0].tempId !== (op && op.tempId)) break;
          writeQueue(key, latest.slice(1));
        }
      }
    } catch (e) {
      // Leave what is left in the queue; the next online event or page load
      // retries it.
    }
  }

  // Shared connectivity state. `navigator.onLine` only reports whether the
  // device has a network interface, so it stays true in a dead zone, behind a
  // captive portal, or during a server outage — exactly when requests are
  // failing. Every request below reports what it actually observed, and any
  // change is broadcast as a `gt-connectivity` window event so the offline
  // banner tracks reachability rather than the browser's guess.
  var reachable = true;

  function isOnline() {
    return navigator.onLine && reachable;
  }

  function setReachable(value) {
    if (reachable === value) return;
    reachable = value;
    window.dispatchEvent(
      new CustomEvent('gt-connectivity', { detail: { online: isOnline() } })
    );
  }

  // The browser regaining an interface is only a hint that the server may be
  // reachable again; assume it is, and let the next failed request say
  // otherwise.
  window.addEventListener('online', function () {
    setReachable(true);
  });

  // request wraps fetch so a transport failure (which fetch reports by
  // rejecting, not by a status code) updates the shared connectivity state.
  async function request(url, options) {
    var res;
    try {
      res = await fetch(url, options);
    } catch (e) {
      setReachable(false);
      throw e;
    }
    setReachable(true);
    return res;
  }

  // api is fetch with the session attached and the two error shapes unwrapped:
  // GolfTrack's own routes answer `{"error": …}`, PocketBase's generated
  // endpoints `{"message": …}` (API.md gap 5), and a page should not care which
  // one it happened to call.
  async function api(url, options) {
    options = options || {};
    var headers = Object.assign(
      { 'Content-Type': 'application/json' },
      options.headers || {}
    );
    var current = token();
    if (current) headers['Authorization'] = current;

    var res = await request(url, Object.assign({}, options, { headers: headers }));

    // A 401 means the token expired or was revoked while the page was open.
    // Sending the player to sign in beats showing them a failure they cannot
    // act on.
    if (res.status === 401) {
      clearToken();
      window.location.href = '/accounts/login/?next=' + encodeURIComponent(window.location.pathname);
      throw new Error('Signed out');
    }

    var data = null;
    var text = await res.text();
    if (text) {
      try {
        data = JSON.parse(text);
      } catch (e) {
        data = null;
      }
    }

    if (!res.ok) {
      var message = (data && (data.error || data.message)) || 'Request failed';
      throw new Error(message);
    }

    return data;
  }

  // download fetches an authenticated route and hands the response to the
  // browser as a file download. Export and template files cannot be plain
  // links: the session rides in the Authorization header, not the URL.
  async function download(url, filename) {
    var headers = {};
    var current = token();
    if (current) headers['Authorization'] = current;
    var res = await request(url, { headers: headers });
    if (!res.ok) {
      var data = null;
      try { data = await res.json(); } catch (e) { data = null; }
      throw new Error((data && (data.error || data.message)) || 'Download failed');
    }
    var blob = await res.blob();
    var objectUrl = URL.createObjectURL(blob);
    var link = document.createElement('a');
    link.href = objectUrl;
    link.download = filename;
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(objectUrl);
  }

  window.gt = {
    token: token,
    clearToken: clearToken,
    api: api,
    download: download,
    isOnline: isOnline,
    cookieName: COOKIE,
    offlineQueue: {
      prefix: QUEUE_PREFIX,
      read: readQueue,
      write: writeQueue,
      drain: drainQueues,
    },
  };

  // Drain queued shots from every page, not just the play page: a golfer who
  // reconnects on the home or review page must not be able to complete the
  // round with shots still stranded in localStorage.
  window.addEventListener('online', function () {
    drainQueues();
  });
  drainQueues();
})();

// --- Alpine components -------------------------------------------------------

// signIn drives the sign-in page. The OAuth2 half runs through the PocketBase
// JS SDK, which opens the provider in a popup and completes the exchange
// against `/api/oauth2-redirect` — the redirect URI AUTH.md already tells the
// owner to register, so no new provider configuration is needed.
function signIn(next) {
  return {
    loading: false,
    error: null,
    email: '',
    password: '',

    client() {
      return new PocketBase(window.location.origin);
    },

    async withProvider(provider) {
      if (this.loading) return;
      this.loading = true;
      this.error = null;
      try {
        const pb = this.client();
        await pb.collection('users').authWithOAuth2({ provider: provider });
        this.finish(pb);
      } catch (e) {
        this.error = e.message || 'Sign-in failed';
        this.loading = false;
      }
    },

    async withPassword() {
      if (this.loading) return;
      this.loading = true;
      this.error = null;
      try {
        const pb = this.client();
        await pb.collection('users').authWithPassword(this.email, this.password);
        this.finish(pb);
      } catch (e) {
        this.error = e.message || 'Sign-in failed';
        this.loading = false;
      }
    },

    // finish writes the session where both halves of the app can read it: the
    // page's own scripts, and the server rendering the next page.
    //
    // `httpOnly` is false because the client has to read the token back out to
    // set the Authorization header — PocketBase's API does not accept the
    // cookie as credentials. That trade-off is the recorded #128 decision.
    // `secure` follows the scheme so the cookie still works on
    // http://localhost during development.
    finish(pb) {
      document.cookie = pb.authStore.exportToCookie(
        {
          httpOnly: false,
          secure: window.location.protocol === 'https:',
          sameSite: 'Lax',
          path: '/',
        },
        window.gt.cookieName
      );
      window.location.href = next || '/';
    },
  };
}

function signOut() {
  return {
    submit() {
      window.gt.clearToken();
      window.location.href = '/';
    },
  };
}

// courseForm submits the course create/edit form to the write routes. The
// whole course — name, hole count and every par — goes in one request, which
// is the reason those routes exist.
function courseForm(config) {
  return {
    holeCount: config.holeCount,
    timeZone: config.timeZone || '',
    timeZoneOptions: [{ value: '', label: 'UTC (not set)' }],
    loading: false,
    error: null,

    init() {
      this.timeZoneOptions = buildTimeZoneOptions(this.timeZone);
    },

    holes() {
      const holes = [];
      for (let number = 1; number <= this.holeCount; number++) {
        const select = document.querySelector('select[data-hole="' + number + '"]');
        if (select) holes.push({ holeNumber: number, par: Number(select.value) });
      }
      return holes;
    },

    async submit() {
      if (this.loading) return;
      this.loading = true;
      this.error = null;
      try {
        const body = JSON.stringify({
          name: document.getElementById('name').value,
          holeCount: this.holeCount,
          timeZone: this.timeZone,
          holes: this.holes(),
        });
        const editing = Boolean(config.courseId);
        const data = await window.gt.api(
          editing ? '/api/courses/' + config.courseId : '/api/courses/',
          { method: editing ? 'PUT' : 'POST', body: body }
        );
        window.location.href = '/courses/' + data.id + '/';
      } catch (e) {
        this.error = e.message;
        this.loading = false;
      }
    },
  };
}

// courseArchiveActions is the archive/restore pair. Both are a single-field
// update, so they stay on the generated PATCH endpoint rather than earning a
// custom route (API.md § "Mapping to the current contract").
function courseArchiveActions(courseId) {
  return {
    loading: false,
    error: null,

    async setArchived(archived, destination) {
      if (this.loading) return;
      this.loading = true;
      this.error = null;
      try {
        await window.gt.api('/api/collections/courses/records/' + courseId, {
          method: 'PATCH',
          body: JSON.stringify({ is_archived: archived }),
        });
        window.location.href = destination;
      } catch (e) {
        this.error = e.message;
        this.loading = false;
      }
    },

    archive() {
      if (!window.confirm('Archive this course? It will no longer appear when starting a round.')) return;
      return this.setArchived(true, '/courses/');
    },

    unarchive(destination) {
      return this.setArchived(false, destination || '/courses/' + courseId + '/');
    },
  };
}

// startRound is the new-round form.
function startRound() {
  return {
    selectedCourse: null,
    playMode: 'full',
    loading: false,
    error: null,

    selectCourse(course) {
      this.selectedCourse = course;
      if (course.holeCount !== 18) this.playMode = 'full';
    },

    async submit() {
      if (!this.selectedCourse || this.loading) return;
      this.loading = true;
      this.error = null;
      try {
        const data = await window.gt.api('/api/rounds/', {
          method: 'POST',
          body: JSON.stringify({
            courseId: this.selectedCourse.id,
            playMode: this.playMode,
          }),
        });
        window.location.href = '/rounds/' + data.id + '/play/';
      } catch (e) {
        this.error = e.message;
        this.loading = false;
      }
    },
  };
}

// reviewForm completes a round from the review page.
function reviewForm(round) {
  return {
    roundId: round.roundId,
    timeZone: round.timeZone || '',
    note: '',
    startedAt: roundInputDateTime(round.startedAt, round.timeZone),
    finishedAt: roundInputDateTime(round.finishedAt, round.timeZone) || currentInputDateTime(round.timeZone),
    loading: false,
    error: null,

    async submit() {
      if (this.loading) return;
      this.loading = true;
      this.error = null;
      try {
        const data = await window.gt.api('/api/rounds/' + this.roundId + '/complete', {
          method: 'POST',
          body: JSON.stringify({
            note: this.note,
            startedAt: this.startedAt,
            finishedAt: this.finishedAt,
          }),
        });
        window.location.href = '/rounds/' + data.id + '/';
      } catch (e) {
        this.error = e.message;
        this.loading = false;
      }
    },
  };
}

// cancelRound is the confirm-and-delete modal shared by the play and review
// pages.
function cancelRound(roundId) {
  return {
    open: false,
    loading: false,

    async confirm() {
      if (this.loading) return;
      this.loading = true;
      try {
        await window.gt.api('/api/rounds/' + roundId + '/cancel', { method: 'POST' });
        window.location.href = '/';
      } catch (e) {
        this.loading = false;
        this.open = false;
      }
    },
  };
}

function deleteRound(roundId) {
  return {
    open: false,
    loading: false,

    async confirm() {
      if (this.loading) return;
      this.loading = true;
      try {
        await window.gt.api('/api/rounds/' + roundId, { method: 'DELETE' });
        window.location.href = '/rounds/';
      } catch (e) {
        this.loading = false;
        this.open = false;
      }
    },
  };
}

// roundsExport drives the export buttons on /settings/export/ (GOL-1).
function roundsExport() {
  return {
    exporting: false,
    error: null,

    async exportRounds(format) {
      if (this.exporting) return;
      this.exporting = true;
      this.error = null;
      try {
        await window.gt.download('/api/rounds/export?format=' + format,
          'golftrack-rounds-' + new Date().toISOString().slice(0, 10) + '.' + format);
      } catch (e) {
        this.error = e.message;
      }
      this.exporting = false;
    },
  };
}

// roundsImport drives the import file input and the template downloads on
// /settings/import/ (GOL-1).
function roundsImport() {
  return {
    importing: false,
    error: null,
    skipped: null,

    async importFile(event) {
      var input = event.target;
      var file = input.files && input.files[0];
      if (!file || this.importing) return;
      this.importing = true;
      this.error = null;
      this.skipped = null;
      try {
        var text = await file.text();
        var data = await window.gt.api('/api/rounds/import', {
          method: 'POST',
          headers: { 'Content-Type': 'text/plain' },
          body: text,
        });
        if (data.imported > 0) {
          // Land on the rounds list, where the imported rounds render
          // server-side like any others.
          window.location.href = '/rounds/';
          return;
        }
        this.skipped = data.skipped;
      } catch (e) {
        this.error = e.message;
      }
      this.importing = false;
      input.value = '';
    },

    async downloadTemplate(format) {
      this.error = null;
      this.skipped = null;
      try {
        await window.gt.download('/api/rounds/import/template?format=' + format,
          'golftrack-import-template.' + format);
      } catch (e) {
        this.error = e.message;
      }
    },
  };
}

function roundInputDateTime(value, timeZone) {
  if (!value || typeof value !== 'string') return '';

  var date = parsePocketBaseDate(value);
  if (!date) {
    var match = value.match(/^(\d{4}-\d{2}-\d{2})[ T](\d{2}:\d{2})/);
    return match ? match[1] + 'T' + match[2] : '';
  }

  return formatDateTime(date, timeZone);
}

function currentInputDateTime(timeZone) {
  return formatDateTime(new Date(), timeZone);
}

function parsePocketBaseDate(value) {
  var normalized = String(value).replace(' ', 'T');
  var date = new Date(normalized);
  return Number.isNaN(date.getTime()) ? null : date;
}

function formatDateTime(date, timeZone) {
  var targetTimeZone = timeZone || 'UTC';
  var options = {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  };
  options.timeZone = targetTimeZone;

  var formatter;
  try {
    formatter = new Intl.DateTimeFormat('en-CA', options);
  } catch (_e) {
    options.timeZone = 'UTC';
    formatter = new Intl.DateTimeFormat('en-CA', options);
  }

  var parts = formatter.formatToParts(date);
  var values = {};
  for (var i = 0; i < parts.length; i++) {
    values[parts[i].type] = parts[i].value;
  }
  return values.year + '-' + values.month + '-' + values.day + 'T' + values.hour + ':' + values.minute;
}

function buildTimeZoneOptions(selectedTimeZone) {
  var preferredNorthAmerica = [
    'America/New_York',
    'America/Chicago',
    'America/Denver',
    'America/Phoenix',
    'America/Los_Angeles',
    'America/Anchorage',
    'Pacific/Honolulu',
    'America/Toronto',
    'America/Vancouver',
    'America/Halifax',
    'America/St_Johns',
  ];

  var seen = Object.create(null);
  var options = [{ value: '', label: 'UTC (not set)' }];

  function pushZone(zone) {
    if (!zone || seen[zone]) return;
    seen[zone] = true;
    options.push({ value: zone, label: zone });
  }

  for (var i = 0; i < preferredNorthAmerica.length; i++) {
    pushZone(preferredNorthAmerica[i]);
  }

  var supported = [];
  if (typeof Intl !== 'undefined' && typeof Intl.supportedValuesOf === 'function') {
    try {
      supported = Intl.supportedValuesOf('timeZone');
    } catch (_e) {
      supported = [];
    }
  }
  for (var j = 0; j < supported.length; j++) {
    pushZone(supported[j]);
  }

  if (selectedTimeZone && !seen[selectedTimeZone]) {
    options.splice(1, 0, { value: selectedTimeZone, label: selectedTimeZone });
  }

  return options;
}

// Register the service worker so the app can cache static assets and serve
// the round-play page when the golfer has no network connection.
//
// A page that installs the worker is fetched before the worker exists, so its
// own HTML never passes through the fetch handler. Once the worker is active
// the page hands its URL over to be cached, otherwise the first offline
// reload after a golfer's first visit would find nothing to fall back to.
if ('serviceWorker' in navigator) {
  window.addEventListener('load', function () {
    var uncontrolled = !navigator.serviceWorker.controller;
    navigator.serviceWorker
      .register('/sw.js')
      .then(function () {
        if (!uncontrolled) return null;
        return navigator.serviceWorker.ready.then(function (registration) {
          var worker = navigator.serviceWorker.controller || registration.active;
          if (worker) {
            worker.postMessage({ type: 'cache-page', url: window.location.href });
          }
        });
      })
      .catch(function () {
        // Fail silently — the page works without the service worker.
      });
  });
}
