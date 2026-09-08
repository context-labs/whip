package expo.modules.whipstorage

import android.system.Os
import android.system.OsConstants
import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition
import java.io.File
import java.io.FileOutputStream

private val databaseFiles = listOf("whip.db", "whip.db-wal", "whip.db-shm", "whip.db-journal")
private const val resetMarker = "reset-pending"

class WhipStorageModule : Module() {
  private fun directory(): File {
    val context = appContext.reactContext ?: error("Whip application context is unavailable")
    val directory = File(context.noBackupFilesDir, "Whip")
    check(directory.isDirectory || directory.mkdirs()) { "Cannot create private Whip storage" }
    return directory
  }

  private fun syncDirectory(directory: File) {
    val descriptor = Os.open(directory.absolutePath, OsConstants.O_RDONLY, 0)
    try {
      // Android's public SDK omits O_DIRECTORY; validate the opened descriptor.
      check(OsConstants.S_ISDIR(Os.fstat(descriptor).st_mode)) { "Whip storage is not a directory" }
      Os.fsync(descriptor)
    } finally { Os.close(descriptor) }
  }

  override fun definition() = ModuleDefinition {
    Name("WhipStorage")

    AsyncFunction("prepareDirectory") {
      val directory = directory()
      mapOf(
        "directory" to directory.absolutePath,
        "databaseExists" to File(directory, "whip.db").exists(),
        "databaseFilesExist" to databaseFiles.any { File(directory, it).exists() },
        "resetPending" to File(directory, resetMarker).exists(),
        "backupExcluded" to true
      )
    }

    AsyncFunction("beginReset") {
      val directory = directory()
      FileOutputStream(File(directory, resetMarker)).use { stream ->
        stream.write("User requested erasure of local Whip data.\n".toByteArray(Charsets.UTF_8))
        stream.fd.sync()
      }
      syncDirectory(directory)
    }

    AsyncFunction("removeDatabaseFiles") {
      val directory = directory()
      check(File(directory, resetMarker).exists()) { "Local data reset has not been confirmed" }
      for (name in databaseFiles) {
        val file = File(directory, name)
        check(!file.isDirectory) { "A database path unexpectedly contains a directory" }
        check(!file.exists() || file.delete()) { "Cannot remove the Whip database file" }
      }
      syncDirectory(directory)
    }

    AsyncFunction("finishReset") {
      val directory = directory()
      check(databaseFiles.none { File(directory, it).exists() }) { "Local data reset has not finished removing the database" }
      val marker = File(directory, resetMarker)
      check(!marker.exists() || marker.delete()) { "Cannot finish local data reset" }
      syncDirectory(directory)
    }
  }
}
