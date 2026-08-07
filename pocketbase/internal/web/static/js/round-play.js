// Alpine.js component for the live round play page.
//
// Ported from static/js/round-play.js with one change: requests go through
// window.gt.api, which attaches the bearer token, instead of through a fetch
// that read Django's CSRF meta tag. The endpoints, the payloads and the
// hole-syncing behaviour are unchanged — they are the same routes the Django
// page called.
//
// Offline support: when the browser has no network connection, addShot stores
// shots in localStorage under a per-round key and displays them optimistically
// as "pending" in the UI. navigateTo updates local state only. undoShot can
// remove the last pending shot. All queued shots are replayed in order when
// the connection comes back, then the page reloads to show the server state.
function roundPlay(initData) {
  return {
    round: initData,
    holes: initData.holes,
    currentHole: initData.currentHole,
    loading: false,
    error: null,
    editingShot: null,
    editClub: '',

    // Offline state
    offline: !navigator.onLine,
    syncing: false,
    queueKey: 'golftrack:offline:' + initData.id,
    queue: [],

    // --- Alpine lifecycle ---

    init() {
      var self = this;
      window.addEventListener('online', function () {
        self.offline = false;
        self.syncQueue();
      });
      window.addEventListener('offline', function () {
        self.offline = true;
      });
      this.loadQueue();
      if (!this.offline && this.queue.length > 0) {
        this.syncQueue();
      }
    },

    // --- Offline queue ---

    loadQueue() {
      try {
        var raw = localStorage.getItem(this.queueKey);
        this.queue = raw ? JSON.parse(raw) : [];
      } catch (e) {
        this.queue = [];
      }
      for (var i = 0; i < this.queue.length; i++) {
        if (this.queue[i].op === 'addShot') {
          this._applyPendingShot(this.queue[i]);
        }
      }
    },

    saveQueue() {
      try {
        localStorage.setItem(this.queueKey, JSON.stringify(this.queue));
      } catch (e) {}
    },

    _applyPendingShot(op) {
      var hole = this.holes.find(function (h) { return h.holeNumber === op.holeNumber; });
      if (!hole) return;
      if (hole.shots.find(function (s) { return s.id === op.tempId; })) return;
      hole.shots = hole.shots.concat([{
        id: op.tempId,
        club: op.club,
        shotNumber: hole.shots.length + 1,
        pending: true,
      }]);
      hole.strokes = hole.shots.length;
      this.holes = this.holes.slice();
    },

    async syncQueue() {
      if (this.syncing || this.queue.length === 0) return;
      this.syncing = true;
      try {
        var ops = this.queue.slice();
        for (var i = 0; i < ops.length; i++) {
          var op = ops[i];
          if (op.op === 'addShot') {
            await window.gt.api(
              `/api/rounds/${this.round.id}/holes/${op.holeNumber}/shots`,
              { method: 'POST', body: JSON.stringify({ club: op.club }) }
            );
            this.queue = this.queue.filter(function (q) { return q !== op; });
            this.saveQueue();
          }
        }
        if (this.queue.length === 0) {
          // All offline shots synced — reload to get authoritative server state.
          window.location.reload();
        }
      } catch (e) {
        // Will retry on the next online event or page load.
      } finally {
        this.syncing = false;
      }
    },

    // --- Computed ---

    get hole() {
      return this.holes.find(h => h.holeNumber === this.currentHole) || null;
    },

    get holeNumbers() {
      return this.holes.map(h => h.holeNumber).sort((a, b) => a - b);
    },

    get canGoPrev() {
      return this.currentHole > this.holeNumbers[0];
    },

    get canGoNext() {
      return this.currentHole < this.holeNumbers[this.holeNumbers.length - 1];
    },

    get totalStrokes() {
      return this.holes.reduce((s, h) => s + h.strokes, 0);
    },

    get hasPendingOnCurrentHole() {
      var hole = this.currentHole;
      return this.queue.some(function (q) {
        return q.op === 'addShot' && q.holeNumber === hole;
      });
    },

    // --- Navigation ---

    async prevHole() {
      const idx = this.holeNumbers.indexOf(this.currentHole);
      if (idx > 0) await this.navigateTo(this.holeNumbers[idx - 1]);
    },

    async nextHole() {
      const idx = this.holeNumbers.indexOf(this.currentHole);
      if (idx < this.holeNumbers.length - 1) await this.navigateTo(this.holeNumbers[idx + 1]);
    },

    async navigateTo(holeNumber) {
      if (this.loading) return;
      this.editingShot = null;
      if (this.offline) {
        // Offline: update local state only; the server will not know the
        // current hole changed until reconnection.
        this.currentHole = holeNumber;
        return;
      }
      this.loading = true;
      this.error = null;
      try {
        const data = await window.gt.api(
          `/api/rounds/${this.round.id}/current-hole`,
          { method: 'PATCH', body: JSON.stringify({ currentHole: holeNumber }) }
        );
        this.currentHole = data.currentHole;
      } catch (e) {
        this.error = e.message;
      } finally {
        this.loading = false;
      }
    },

    // --- Shot management ---

    async addShot(club) {
      if (this.loading) return;
      this.error = null;

      if (this.offline) {
        var tempId = 'tmp-' + Date.now() + '-' + Math.random().toString(36).slice(2);
        var op = { op: 'addShot', holeNumber: this.currentHole, club: club, tempId: tempId };
        this.queue = this.queue.concat([op]);
        this.saveQueue();
        this._applyPendingShot(op);
        return;
      }

      this.loading = true;
      try {
        const data = await window.gt.api(
          `/api/rounds/${this.round.id}/holes/${this.currentHole}/shots`,
          { method: 'POST', body: JSON.stringify({ club }) }
        );
        this.syncHole(data);
      } catch (e) {
        this.error = e.message;
      } finally {
        this.loading = false;
      }
    },

    async undoShot() {
      if (this.loading) return;
      this.error = null;

      if (this.offline) {
        // Offline undo: remove the last pending shot on this hole.
        var holeNumber = this.currentHole;
        var reversed = this.queue.slice().reverse();
        var lastPending = null;
        for (var i = 0; i < reversed.length; i++) {
          if (reversed[i].op === 'addShot' && reversed[i].holeNumber === holeNumber) {
            lastPending = reversed[i];
            break;
          }
        }
        if (!lastPending) {
          this.error = 'Go online to undo shots that were saved before you went offline';
          return;
        }
        var tempId = lastPending.tempId;
        this.queue = this.queue.filter(function (q) { return q !== lastPending; });
        this.saveQueue();
        var hole = this.holes.find(function (h) { return h.holeNumber === holeNumber; });
        if (hole) {
          hole.shots = hole.shots.filter(function (s) { return s.id !== tempId; });
          hole.strokes = hole.shots.length;
          this.holes = this.holes.slice();
        }
        return;
      }

      this.loading = true;
      try {
        const data = await window.gt.api(
          `/api/rounds/${this.round.id}/holes/${this.currentHole}/undo`,
          { method: 'POST', body: '{}' }
        );
        this.syncHole(data);
      } catch (e) {
        this.error = e.message;
      } finally {
        this.loading = false;
      }
    },

    startEdit(shot) {
      this.editingShot = shot.id;
      this.editClub = shot.club;
    },

    cancelEdit() {
      this.editingShot = null;
      this.editClub = '';
    },

    async saveEdit(shotId) {
      if (this.loading) return;
      this.loading = true;
      this.error = null;
      try {
        const updated = await window.gt.api(
          `/api/rounds/${this.round.id}/holes/${this.currentHole}/shots/${shotId}`,
          { method: 'PATCH', body: JSON.stringify({ club: this.editClub }) }
        );
        const hole = this.holes.find(h => h.holeNumber === this.currentHole);
        if (hole) {
          const idx = hole.shots.findIndex(s => s.id === shotId);
          if (idx >= 0) hole.shots[idx] = updated;
        }
        this.editingShot = null;
        this.editClub = '';
      } catch (e) {
        this.error = e.message;
      } finally {
        this.loading = false;
      }
    },

    async deleteShot(shotId) {
      if (this.loading) return;
      this.loading = true;
      this.error = null;
      try {
        const data = await window.gt.api(
          `/api/rounds/${this.round.id}/holes/${this.currentHole}/shots/${shotId}`,
          { method: 'DELETE' }
        );
        this.syncHole(data);
        this.editingShot = null;
      } catch (e) {
        this.error = e.message;
      } finally {
        this.loading = false;
      }
    },

    // --- Helpers ---

    syncHole(holeData) {
      const idx = this.holes.findIndex(h => h.holeNumber === holeData.holeNumber);
      if (idx >= 0) {
        this.holes[idx] = holeData;
        this.holes = [...this.holes]; // trigger Alpine reactivity
      }
    },
  };
}
