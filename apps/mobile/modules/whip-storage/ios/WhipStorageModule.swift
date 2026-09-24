import ExpoModulesCore
import Foundation
import Darwin

private let databaseFiles = ["whip.db", "whip.db-wal", "whip.db-shm", "whip.db-journal"]
private let resetMarker = "reset-pending"

public final class WhipStorageModule: Module {
  private static func directory() throws -> URL {
    let manager = FileManager.default
    var directory = try manager.url(
      for: .applicationSupportDirectory, in: .userDomainMask,
      appropriateFor: nil, create: true
    ).appendingPathComponent("Whip", isDirectory: true)
    try manager.createDirectory(at: directory, withIntermediateDirectories: true)
    // Excluding the directory also excludes SQLCipher sidecars and reset marker.
    var values = URLResourceValues()
    values.isExcludedFromBackup = true
    try directory.setResourceValues(values)
    guard try directory.resourceValues(forKeys: [.isExcludedFromBackupKey]).isExcludedFromBackup == true else {
      throw NSError(domain: "WhipStorage", code: 1, userInfo: [NSLocalizedDescriptionKey: "Cannot exclude Whip storage from backups"])
    }
    try manager.setAttributes(
      [.protectionKey: FileProtectionType.completeUntilFirstUserAuthentication],
      ofItemAtPath: directory.path
    )
    return directory
  }

  private static func syncDirectory(_ directory: URL) throws {
    let descriptor = Darwin.open(directory.path, O_RDONLY)
    guard descriptor >= 0 else { throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO) }
    defer { Darwin.close(descriptor) }
    guard Darwin.fsync(descriptor) == 0 else { throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO) }
  }

  public func definition() -> ModuleDefinition {
    Name("WhipStorage")

    AsyncFunction("prepareDirectory") { () throws -> [String: Any] in
      let directory = try Self.directory()
      let exists = { (name: String) in FileManager.default.fileExists(atPath: directory.appendingPathComponent(name).path) }
      return [
        "directory": directory.path,
        "databaseExists": exists("whip.db"),
        "databaseFilesExist": databaseFiles.contains(where: exists),
        "resetPending": exists(resetMarker),
        "backupExcluded": true
      ]
    }

    // JavaScript requires explicit user confirmation and closes SQLite before
    // calling these fixed-path operations. The marker survives interrupted reset.
    AsyncFunction("beginReset") { () throws -> Void in
      let directory = try Self.directory()
      let marker = directory.appendingPathComponent(resetMarker)
      try Data("User requested erasure of local Whip data.\n".utf8).write(to: marker, options: .atomic)
      let file = try FileHandle(forWritingTo: marker)
      defer { try? file.close() }
      try file.synchronize()
      try Self.syncDirectory(directory)
    }

    AsyncFunction("removeDatabaseFiles") { () throws -> Void in
      let directory = try Self.directory()
      let manager = FileManager.default
      guard manager.fileExists(atPath: directory.appendingPathComponent(resetMarker).path) else {
        throw NSError(domain: "WhipStorage", code: 2, userInfo: [NSLocalizedDescriptionKey: "Local data reset has not been confirmed"])
      }
      for name in databaseFiles {
        let file = directory.appendingPathComponent(name)
        var isDirectory: ObjCBool = false
        if manager.fileExists(atPath: file.path, isDirectory: &isDirectory) {
          guard !isDirectory.boolValue else {
            throw NSError(domain: "WhipStorage", code: 3, userInfo: [NSLocalizedDescriptionKey: "A database path unexpectedly contains a directory"])
          }
          try manager.removeItem(at: file)
        }
      }
      try Self.syncDirectory(directory)
    }

    AsyncFunction("finishReset") { () throws -> Void in
      let directory = try Self.directory()
      let manager = FileManager.default
      guard !databaseFiles.contains(where: { manager.fileExists(atPath: directory.appendingPathComponent($0).path) }) else {
        throw NSError(domain: "WhipStorage", code: 4, userInfo: [NSLocalizedDescriptionKey: "Local data reset has not finished removing the database"])
      }
      let marker = directory.appendingPathComponent(resetMarker)
      if manager.fileExists(atPath: marker.path) { try manager.removeItem(at: marker) }
      try Self.syncDirectory(directory)
    }
  }
}
