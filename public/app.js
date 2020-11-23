Vue.config.devtools = true
const app = new Vue({
  computed: {
    clock() {
      return moment(this.time).format('HH:mm:ss')
    },

    icons() {
      const icons = []

      if (!this.conn.avail) {
        icons.push({ class: 'fas fa-ethernet text-warning' })
      }

      return icons
    },

    nextFollowers() {
      return Math.ceil((this.store.followers.count + 1) / 25) * 25
    },

    nextSubs() {
      return Math.ceil((this.store.subs.count + 1) / 5) * 5
    },
  },

  created() {
    window.setInterval(() => { this.time = new Date() }, 1000)
    this.startSocket()
  },

  data: {
    conn: {
      avail: false,
      backoff: 100,
    },
    store: {},
    socket: null,
    time: new Date(),
    version: null,
  },

  el: '#app',

  methods: {
    showAlert(title, text) {
      this.$bvToast.toast(text, {
        title,
        toaster: 'b-toaster-top-right',
        variant: 'primary',
      })
    },

    startSocket() {
      if (this.socket) {
        // Dispose old socket
        this.socket.close()
        this.socket = null
      }

      let socketAddr = `${window.location.origin.replace(/^http/, 'ws')}/api/subscribe`

      this.socket = new WebSocket(socketAddr)
      this.socket.onclose = () => {
        this.conn.avail = false
        this.conn.backoff = Math.min(this.conn.backoff * 1.25, 10000)
        window.setTimeout(this.startSocket, this.conn.backoff) // Restart socket
      }
      this.socket.onmessage = evt => {
        const data = JSON.parse(evt.data)

        if (data.version) {
          this.version = data.version
        }

        switch (data.type) {
          case 'store':
            this.store = data.payload
            break

          default:
            console.log(`Unhandled message type ${data.type}`, data)
        }
      }
      this.socket.onopen = evt => {
        this.conn.avail = true
        this.conn.backoff = 100
      }
    },
  },

  watch: {
    'store.followers.last'(to, from) {
      if (!from || !to) {
        // Initial load
        return
      }
      this.showAlert('New Follower', `${to} just followed`)
    },

    version(to, from) {
      if (!from || !to || from === to) {
        return
      }
      window.location.reload()
    },
  },
})
