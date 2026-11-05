/**
 * @typedef {{path: string, state: string}} Output
 * @typedef {{id: string, title: string, status: string, percent: number|null, error: string|null, outputs: Output[]|null}} Job
 */

/**
 * Send a request to the LemonTube API.
 * @param {string} path
 * @param {RequestInit} [options]
 * @returns {Promise<any>}
 */
async function api(path, options) {
  const response = await fetch(path, options);
  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    throw new Error(body.message || `Request failed (${response.status})`);
  }
  return response.status === 204 ? null : response.json();
}

/** @returns {object} Alpine component for the download page. */
function app() {
  return {
    url: '',
    downloadType: 'video',
    format: 'mp4',
    quality: 'best',
    subtitleLangs: '',
    adding: false,
    error: '',
    /** @type {Job[]} */
    jobs: [],
    /** @type {string[]} */
    cookieDomains: [],
    cookieText: '',
    cookieFileName: '',
    cookieDragging: false,
    ytdlp: {version: '', source: '', can_update: false},
    ytdlpMessage: '',
    updatingYTDLP: false,
    clearingOrphans: false,
    orphanMessage: '',
    /** @type {string|null} */
    openLogId: null,
    /** @type {string[]} */
    logLines: [],

    /** @returns {{value: string, label: string}[]} */
    get formats() {
      const values = this.downloadType === 'audio' ? ['mp3', 'm4a', 'opus', 'wav', 'flac'] : ['mp4', 'any'];
      return values.map(value => ({value, label: value === 'any' ? 'Any format' : value.toUpperCase()}));
    },

    /** Start polling when Alpine initializes the component. @returns {Promise<void>} */
    async init() {
      await Promise.all([this.refresh(), this.refreshCookies(), this.refreshYTDLP()]);
      this.timer = setInterval(() => {
        if (!document.hidden) this.refresh();
      }, 3000);
    },

    /** Stop polling when Alpine removes the component. @returns {void} */
    destroy() {
      clearInterval(this.timer);
    },

    /** @returns {Promise<void>} */
    async refresh() {
      try {
        const jobs = await api('/api/jobs');
        this.jobs = jobs.reverse();
        if (this.openLogId) {
          this.logLines = await api(`/api/jobs/${encodeURIComponent(this.openLogId)}/logs`);
        }
      } catch (error) {
        this.error = error.message;
      }
    },

    /** @returns {Promise<void>} */
    async addDownload() {
      this.adding = true;
      this.error = '';
      try {
        await api('/api/jobs', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({
            url: this.url.trim(),
            download_type: this.downloadType,
            options: {
              format: this.format,
              quality: this.quality,
              subtitle_langs: this.subtitleLangs.split(',').map(value => value.trim()).filter(Boolean),
            },
          }),
        });
        this.url = '';
        await this.refresh();
      } catch (error) {
        this.error = error.message;
      } finally {
        this.adding = false;
      }
    },

    /** @param {Job} job @returns {boolean} */
    isActive(job) {
      return job.status === 'queued' || job.status === 'running';
    },

    /** @param {Job} job @returns {string} */
    jobStatus(job) {
      const progress = job.status === 'running' && job.percent != null ? ` · ${Math.round(job.percent)}%` : '';
      const labels = {queued: 'Queued', running: 'Downloading', finished: 'Finished', failed: 'Failed', canceled: 'Canceled'};
      return `${labels[job.status] || job.status}${progress}`;
    },

    /** @param {Job} job @returns {Output[]} */
    publishedOutputs(job) {
      return (job.outputs || []).filter(output => output.state === 'published');
    },

    /** @param {string} id @returns {Promise<void>} */
    async deleteJob(id) {
      try {
        await api(`/api/jobs/${encodeURIComponent(id)}`, {method: 'DELETE'});
        if (this.openLogId === id) this.openLogId = null;
        await this.refresh();
      } catch (error) {
        this.error = error.message;
      }
    },

    /** @param {string} id @returns {Promise<void>} */
    async retryJob(id) {
      try {
        await api(`/api/jobs/${encodeURIComponent(id)}/retry`, {method: 'POST'});
        await this.refresh();
      } catch (error) {
        this.error = error.message;
      }
    },

    /** @param {string} id @returns {Promise<void>} */
    async toggleLog(id) {
      this.openLogId = this.openLogId === id ? null : id;
      this.logLines = [];
      if (this.openLogId) await this.refresh();
    },

    /** @returns {Promise<void>} */
    async refreshCookies() {
      try {
        this.cookieDomains = await api('/api/cookies');
      } catch (error) {
        this.error = error.message;
      }
    },

    /** @param {File|undefined} file @returns {Promise<void>} */
    async readCookieFile(file) {
      if (!file) return;
      if (file.size > 1_000_000) {
        this.error = 'Cookie file exceeds 1 MB.';
        return;
      }
      try {
        this.cookieText = await file.text();
        this.cookieFileName = file.name;
        this.$refs.cookieFile.value = '';
        this.error = '';
      } catch (error) {
        this.error = error.message;
      }
    },

    /** @param {DragEvent} event @returns {Promise<void>} */
    async readCookieDrop(event) {
      this.cookieDragging = false;
      await this.readCookieFile(event.dataTransfer?.files[0]);
    },

    /** @param {ClipboardEvent} event @returns {Promise<void>} */
    async readCookiePaste(event) {
      const file = event.clipboardData?.files[0];
      if (!file) return;
      event.preventDefault();
      await this.readCookieFile(file);
    },

    /** @returns {Promise<void>} */
    async uploadCookies() {
      try {
        if (!this.cookieText.trim()) throw new Error('Add cookie file contents first.');
        await api('/api/cookies', {method: 'POST', body: this.cookieText});
        this.cookieText = '';
        this.cookieFileName = '';
        this.$refs.cookieFile.value = '';
        await this.refreshCookies();
      } catch (error) {
        this.error = error.message;
      }
    },

    /** @returns {Promise<void>} */
    async clearCookies() {
      try {
        await api('/api/cookies', {method: 'DELETE'});
        await this.refreshCookies();
      } catch (error) {
        this.error = error.message;
      }
    },

    /** @returns {Promise<void>} */
    async clearOrphans() {
      if (!window.confirm('Delete unchanged files from removed downloads? This cannot be undone.')) return;
      this.clearingOrphans = true;
      this.orphanMessage = '';
      try {
        const result = await api('/api/orphan-outputs/clear', {method: 'POST'});
        this.orphanMessage = `Cleared files for ${result.cleared} removed downloads.` +
          (result.retained ? ` ${result.retained} could not be cleared; changed files are kept.` : '');
      } catch (error) {
        this.orphanMessage = error.message;
      } finally {
        this.clearingOrphans = false;
      }
    },

    /** @returns {Promise<void>} */
    async refreshYTDLP() {
      try {
        this.ytdlp = await api('/api/yt-dlp');
        this.ytdlpMessage = this.ytdlp.error || '';
      } catch (error) {
        this.ytdlpMessage = error.message;
      }
    },

    /** @returns {Promise<void>} */
    async updateYTDLP() {
      this.updatingYTDLP = true;
      this.ytdlpMessage = 'Downloading the latest nightly build…';
      try {
        this.ytdlp = await api('/api/yt-dlp/update', {method: 'POST'});
        this.ytdlpMessage = 'yt-dlp updated.';
      } catch (error) {
        this.ytdlpMessage = error.message;
      } finally {
        this.updatingYTDLP = false;
      }
    },
  };
}
