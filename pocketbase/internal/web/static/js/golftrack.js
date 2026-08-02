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

    var res = await fetch(url, Object.assign({}, options, { headers: headers }));

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

  window.gt = { token: token, clearToken: clearToken, api: api, cookieName: COOKIE };
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
    loading: false,
    error: null,

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
    note: '',
    startedAt: roundInputDateTime(round.startedAt),
    finishedAt: roundInputDateTime(round.finishedAt) || currentInputDateTime(),
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

function roundInputDateTime(value) {
  if (!value || typeof value !== 'string') return '';

  var match = value.match(/^(\d{4}-\d{2}-\d{2})[ T](\d{2}:\d{2})/);
  return match ? match[1] + 'T' + match[2] : '';
}

function currentInputDateTime() {
  var now = new Date();
  var year = String(now.getFullYear());
  var month = String(now.getMonth() + 1).padStart(2, '0');
  var day = String(now.getDate()).padStart(2, '0');
  var hour = String(now.getHours()).padStart(2, '0');
  var minute = String(now.getMinutes()).padStart(2, '0');
  return year + '-' + month + '-' + day + 'T' + hour + ':' + minute;
}
