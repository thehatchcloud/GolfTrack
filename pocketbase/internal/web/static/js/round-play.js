// Alpine.js component for the live round play page.
//
// Ported from static/js/round-play.js with one change: requests go through
// window.gt.api, which attaches the bearer token, instead of through a fetch
// that read Django's CSRF meta tag. The endpoints, the payloads and the
// hole-syncing behaviour are unchanged — they are the same routes the Django
// page called.
function roundPlay(initData) {
  return {
    round: initData,
    holes: initData.holes,
    currentHole: initData.currentHole,
    loading: false,
    error: null,
    editingShot: null,
    editClub: '',

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
      this.loading = true;
      this.error = null;
      try {
        const data = await window.gt.api(
          `/api/rounds/${this.round.id}/current-hole`,
          { method: 'PATCH', body: JSON.stringify({ currentHole: holeNumber }) }
        );
        this.currentHole = data.currentHole;
        this.editingShot = null;
      } catch (e) {
        this.error = e.message;
      } finally {
        this.loading = false;
      }
    },

    // --- Shot management ---

    async addShot(club) {
      if (this.loading) return;
      this.loading = true;
      this.error = null;
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
      this.loading = true;
      this.error = null;
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
