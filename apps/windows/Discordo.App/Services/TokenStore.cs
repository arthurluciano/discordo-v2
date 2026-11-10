using Windows.Security.Credentials;

namespace Discordo.App.Services;

/// <summary>Keeps the session token in the Windows Credential Locker (encrypted per user), never in a plain file.</summary>
internal static class TokenStore
{
    private const string Resource = "Discordo", UserName = "session";

    public static string? Load()
    {
        try
        {
            var credential = new PasswordVault().Retrieve(Resource, UserName);
            credential.RetrievePassword();
            return credential.Password;
        }
        catch (Exception) // Retrieve throws when there is nothing stored
        {
            return null;
        }
    }

    public static void Save(string token) => new PasswordVault().Add(new PasswordCredential(Resource, UserName, token));

    public static void Clear()
    {
        try
        {
            var vault = new PasswordVault();
            vault.Remove(vault.Retrieve(Resource, UserName));
        }
        catch (Exception)
        {
            // Nothing stored: already clear.
        }
    }
}
