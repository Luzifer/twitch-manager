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

    this.sound = new Audio()
    this.sound.addEventListener('load', () => this.sound.play(), true)
    this.sound.autoplay = true
  },

  data: {
    conn: {
      avail: false,
      backoff: 100,
    },
    firstLoad: true,
    sound: null,
    store: {},
    socket: null,
    time: new Date(),
    version: null,
  },

  el: '#app',

  methods: {
    playSound(soundUrl) {
      this.sound.src = soundUrl
    },

    showAlert(title, text, variant) {
      this.$bvToast.toast(text, {
        title,
        toaster: 'b-toaster-top-right',
        variant: variant || 'primary',
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
          case 'alert':
            this.showAlert(data.payload.title, data.payload.text, data.payload.variant)
            if (data.payload.sound) {
              this.playSound(data.payload.sound)
            }
            break

          case 'raid':
            this.showAlert('Incoming raid', `${data.payload.from} just raided with ${data.payload.viewerCount} raiders`)
            break

          case 'store':
            this.store = data.payload
            window.setTimeout(() => { this.firstLoad = false }, 100) // Delayed to let the watches trigger
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
      if (this.firstLoad || !to) {
        // Initial load or no follower
        return
      }
      this.showAlert('New Follower', `${to} just followed`, 'success')
      this.playSound('/public/doorbell.webm')
    },

    version(to, from) {
      if (!from || !to || from === to) {
        return
      }
      window.location.reload()
    },
  },
})
