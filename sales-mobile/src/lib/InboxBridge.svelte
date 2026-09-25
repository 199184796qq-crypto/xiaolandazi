<script lang="ts">
import {onMount} from 'svelte';
import {session} from '$lib/session';
import {startWorkInbox,stopWorkInbox} from '$lib/workInbox';
onMount(()=>{const off=session.subscribe(v=>startWorkInbox(v.bootstrap?.actor?.user_id?JSON.stringify([v.bootstrap.actor,v.bootstrap.staff_access]):''));return()=>{off();stopWorkInbox()}});
</script>
